"""Volume scale conversion between the device's native level and HA's float.

Split out as pure logic for the reason em_linkauth.py was: the three call
sites (em_controller, em_esphome, em_api) each had their OWN copy of
`level / 175`, so the scale lived in three places and none of them were
covered by a test. Changing the ceiling meant finding all three.

The device level is the raw tinymix ctl 61 index — the tlv320aic32x4 DAC
digital volume, 0.5dB per step with 0dB (unity) at index 127. The scale is
therefore dB-linear, which is roughly perceptually linear, so the mapping to
HA's 0.0–1.0 is a plain proportion and needs no extra taper.

DEVICE_VOLUME_MAX is 127 and NOT the control's own maximum of 175. Indexes
above 127 apply positive digital gain to near-full-scale PCM and saturate
inside the DAC — measured at 65% THD by index 153 (see
device/internal/server/volume.go for the full measurement and why stock
FireOS never touches this control).

The ratio this module reports will NOT agree with a source's own slider, and
that is a decided trade rather than a bug to fix. Measured against four pairs
off one device: the Spotify slider at 14% arrives as level 93, which the ring,
the dashboard and HA's `volume_level` all render as 73%. Both numbers are
true. −17dB is 14% of full amplitude and 73% of the way up a 63.5dB scale;
Spotify's slider is a linear amplitude fraction and this one is a position in
dB.

Showing the amplitude fraction instead (`10**((level-127)/40)`) would make the
two agree and was declined by the device's owner, because the cost lands on
the half that gets used every day: one press of the volume button down from
full would move the ring from 100% to 63%, and the whole bottom of the scale
would bunch into the last few pixels. A dB-linear arc moves evenly under a
finger.

The consequence to know about is that an HA automation reading `volume_level`
inherits it — 0.8 there is a dB position, not four fifths of the amplitude.
Anything comparing the two scales has to convert with `level_to_db` rather
than assume they are the same number.

Fixing this per-source is the option that looks obvious and is worse than
either consistent answer: the ring would then mean something different
depending on who last touched it.
"""

# Codec unity gain. The mixer control accepts up to 175; everything above
# this clips. See the module docstring.
DEVICE_VOLUME_MAX = 127

# 0.5dB per index step, 0dB at DEVICE_VOLUME_MAX.
DB_PER_STEP = 0.5


def level_to_db(level: int) -> float:
    """Device level as dB relative to unity. Index 127 -> 0.0, index 0 -> -63.5."""
    return (level - DEVICE_VOLUME_MAX) * DB_PER_STEP


def device_level_to_ha(level: int) -> float:
    """Convert a device volume level to an HA float (0.0–1.0)."""
    try:
        return max(0.0, min(1.0, float(level) / DEVICE_VOLUME_MAX))
    except (TypeError, ValueError, ZeroDivisionError):
        return 0.0


def ha_volume_to_device(volume: float) -> int:
    """Convert an HA volume float (0.0–1.0) to a device volume level.

    Clamped to the codec's unity gain, so HA asking for full volume can never
    put the DAC into positive digital gain.
    """
    return max(0, min(DEVICE_VOLUME_MAX, round(float(volume) * DEVICE_VOLUME_MAX)))
