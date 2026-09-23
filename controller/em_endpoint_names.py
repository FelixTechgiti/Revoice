"""
What an Echo calls itself in Spotify and in AirPlay.

`spotifyName` and `airplayName` default to empty, and empty means the
controller pushes nothing — so the endpoint keeps the name it was constructed
with, which on the device is `deviceID`, which is the serial. Somebody who has
labelled their Echo `Küche` finds `G090LF11743202AM` in the Spotify picker,
and with two Dots the lists fill with serials differing in the middle.

The controller has known the answer the whole time. `em_db`'s own comment
above the default says so:

    spotifyName is pushed rather than left to the device because the
    controller knows the LABEL and the device knows only its serial, and
    "G090LF1180570SPJ" is not a speaker anybody picks out of a list.

The reasoning is right and the empty default defeats it. Doing it on the
device instead cannot work: `cmd/server.go` has only `deviceID`, which is the
serial — that asymmetry is the whole reason this lives here.

# Resolved at PUSH time, never stored

The stored value stays empty, and that is the load-bearing part rather than an
implementation detail. Filling the database with the label would make the two
names diverge the moment somebody renames the device — the endpoint would keep
announcing whatever the label happened to be on the day it was first pushed,
with no way to tell a deliberate name from a copied one. It would also put the
label into the dashboard's own field, where saving it turns an inherited
default into an explicit setting nobody chose.

Empty therefore keeps meaning "follow the label", and anybody who wants the
Spotify name to differ types one.

# Whitespace, and why an empty label is left alone

A label of nothing but spaces is not a name. Falling back to it would produce
an endpoint advertising a blank string, which is worse than the serial: at
least a serial identifies the box. So a label that strips to nothing leaves
the key exactly as it was, and the device goes on doing what it does today.
"""


# The keys this applies to. Both are DEVICE_ONLY_KEYS in em_config_sections
# for a related reason — a name's job is to tell two devices apart — and the
# lists are deliberately separate: that one is about scoping, this one is
# about what an empty value means.
NAME_KEYS: tuple[str, ...] = ("spotifyName", "airplayName")


def resolve(config: dict, label: str) -> dict:
    """
    A copy of `config` with each empty endpoint name filled from the label.

    A copy rather than an edit in place: callers pass the effective config,
    which is also what gets mirrored onto the live Device and returned to the
    dashboard, and a fallback that leaked into either would be indistinguishable
    from a stored value.

    A key that is ABSENT stays absent. The config push treats a missing key as
    "leave this alone", so inventing one here would start pushing a name at
    firmware that never asked for it.
    """
    name = (label or "").strip()
    if not name:
        return dict(config)
    out = dict(config)
    for key in NAME_KEYS:
        if key in out and not str(out[key] or "").strip():
            out[key] = name
    return out
