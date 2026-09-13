"""A device already on emOS can be its own build reference (#156).

    python -m pytest tests/test_emos_self_reference.py

**This is the property the whole network re-provision rests on**, and it is
worth proving before anything is built on top of it.

Re-flashing a device over the network needs a reference boot image to rebuild
from — and the controller has none. `_post_provision_emos_image` says so
outright and means it: *"NOTHING IS STORED. The reference is the user's own
boot partition and the only copy that matters is the one the wizard escrowed
to them."* That is the same rule that stops emOS publishing a bootable image,
so the answer cannot be to start keeping one.

The way through is that `split_reference` accepts any `ANDROID!` image and
reads the kernel, the DTBs, the load addresses and the cmdline out of it,
while `build_emos_image` replaces only the ramdisk. An image this packer built
is itself such an image, carrying the same kernel — so **the partition a
device is booted from can supply the reference for its next image**. Nothing
is stored and nothing is redistributed; the kernel and DTBs came off the stock
image once and are carried forward from there.

`test_emos_build.py` covers ONE generation — the built image is a boot image
the packer understands, and the kernel survives it. Every case here is about
what happens when the output goes back in as the input, which is what a
network re-provision does and what it does for ever afterwards.

The fixture is synthetic, exactly as that file's is. Note what none of this
proves: that the resulting image BOOTS. That needs a device, and it is
increment 4 on #156 — the one that can brick.
"""

import hashlib
import importlib.util
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import em_emos_build as eb  # noqa: E402

# The fixture lives in the existing suite; importing it rather than copying it
# keeps one definition of what a biscuit boot image looks like. A second copy
# would drift the day the real layout is corrected, and drift SILENTLY, since
# both copies would still round-trip against themselves.
_spec = importlib.util.spec_from_file_location(
    "_emos_build_fixtures", Path(__file__).with_name("test_emos_build.py"))
_fixtures = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_fixtures)

make_reference = _fixtures.make_reference
fake_init = _fixtures.fake_init


def test_an_emos_image_is_accepted_as_a_reference():
    """The first thing that has to be true, and the cheapest to get wrong.

    If the packer refused its own output there would be no network
    re-provision at all without storing a stock image somewhere — which is
    precisely what the rule forbids.
    """
    built = eb.build_emos_image(make_reference(), fake_init(), "0.4")["image"]
    again = eb.build_emos_image(built, fake_init(), "0.4")
    assert again["image"] == built, (
        "rebuilding from an emOS image did not reproduce it, so a device "
        "cannot be its own reference")


def test_the_kernel_survives_being_carried_forward_repeatedly():
    """What the reference is FOR, over a chain rather than a pair.

    The ramdisk is replaced every time; the kernel and the device trees are
    the part that came off the stock image and can never be regenerated. Once
    updates rebuild from the previous image, every future device carries a
    kernel that has been through the packer N times — so a loss of one byte
    per generation is a device that boots today and does not next year.
    """
    ref = make_reference()
    stock = eb.split_reference(ref)

    image = ref
    for _ in range(5):
        image = eb.build_emos_image(image, fake_init(), "0.4")["image"]

    carried = eb.split_reference(image)
    assert carried["zimage"] == stock["zimage"]
    assert carried["dtbs"] == stock["dtbs"]
    assert carried["kaddr"] == stock["kaddr"]


def test_the_cmdline_does_not_grow_across_generations():
    """The one thing known to accumulate.

    `pack` appends the ramoops parameters, and the existing suite pins that
    packing twice does not append them twice. A network re-provision makes
    that N times rather than twice, so the property has to hold for a chain.
    A cmdline that grew a little each time would overrun its 512-byte field
    eventually, and the failure lands on whoever updates most often.
    """
    image = make_reference()
    lengths = []
    for _ in range(5):
        info = eb.build_emos_image(image, fake_init(), "0.4")
        image = info["image"]
        lengths.append(len(info["cmdline"]))

    assert len(set(lengths)) == 1, (
        f"the cmdline changed length across rebuilds: {lengths}")


def test_a_new_init_changes_the_image_and_leaves_the_kernel_alone():
    """An update is a new ramdisk over the same kernel.

    Both halves matter: an image that did not change would mean the update
    silently did nothing, and a kernel that changed would mean the packer had
    rebuilt something it has no source for.
    """
    ref = make_reference()
    first = eb.build_emos_image(ref, fake_init(), "0.4")
    second = eb.build_emos_image(first["image"], fake_init(size=8192), "0.5")

    assert second["image"] != first["image"]
    assert second["zimage_size"] == first["zimage_size"]
    assert second["dtb_size"] == first["dtb_size"]
    assert eb.split_reference(second["image"])["zimage"] == \
        eb.split_reference(ref)["zimage"]


def test_the_reported_reference_md5_describes_the_input():
    """The receipt has to describe what was rebuilt FROM.

    A network re-provision has no operator watching a file dialog, so this
    digest is the only record of which image a build started from. Reporting
    the built image's own digest there would look right in every log and
    answer a different question — and on a chain the two are never equal, so
    the mistake is invisible on a single build and permanent afterwards.
    """
    ref = make_reference()
    first = eb.build_emos_image(ref, fake_init(), "0.4")
    assert first["reference_md5"] == hashlib.md5(ref).hexdigest()

    second = eb.build_emos_image(first["image"], fake_init(), "0.4")
    assert second["reference_md5"] == hashlib.md5(first["image"]).hexdigest()
    assert second["reference_md5"] != first["reference_md5"]
