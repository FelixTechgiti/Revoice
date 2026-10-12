# Die Schnittstelle zwischen Gerät und Controller

Das ist der Vertrag, den ein Geräte-Binary umsetzt, um vom
Revoice-Controller angesteuert zu werden. Er existiert, damit ein **neues
Board** — der Echo Show 8 (`crown`) ist das erste nach dem Echo Dot
(`biscuit`) — gegen eine geschriebene Spezifikation gebaut werden kann statt
durch Lesen des Dot-Quelltexts. Der Controller ist bereits boardunabhängig;
ein neues Board ist fast vollständig ein frischer Satz
Hardware-**Bindings** hinter demselben Wire-Protokoll.

Maßgeblich für jede Nachricht und jedes Feld ist der inline zitierte Code; wo
dieses Dokument und der Code sich widersprechen, gewinnt der Code, und dieses
Dokument ist der Fehler.

## Die zwei Regeln, denen alles andere dient

Beide werden von `controller/tests/test_capabilities.py` bewacht. Ein Board,
das sie einhält, kann sich mit jeder Controller-Version paaren und umgekehrt.

- **Nach Fähigkeit aushandeln, nicht nach Version.** Das Gerät kündigt in
  seiner `register`-Nachricht an, was es umsetzt; der Controller liest
  `Device.capabilities` und macht jede Funktion von der passenden Zeichenkette
  abhängig. Niemals Versionszeichenketten vergleichen — das packt
  Release-Geschichte in den Controller und schätzt Dev-Builds falsch ein. Ein
  Bedienelement, dessen Fähigkeit dem Gerät fehlt, wird **deaktiviert mit
  Begründung** angezeigt, nie als Element, das stillschweigend nichts tut.
- **Auf altes Verhalten zurückfallen, nie auf eine falsche Antwort.**
  Unbekannte JSON-Felder und Nachrichtentypen werden in beide Richtungen
  ignoriert. Wo ein neues Feld eine Messung festhält, wird Abwesenheit als
  **NULL gespeichert, nicht als 0** — ein Gerät, das etwas nicht melden kann,
  darf nicht so gelesen werden, als hätte es null gemeldet.

## Die drei Ebenen

Jedes Gerät öffnet **drei** WebSocket-Verbindungen zum Controller. Der
Controller wird per mDNS gefunden (`_emcontroller._tcp.local`); das Gerät
wählt alle drei nach außen.

| Pfad | Nutzlast | Richtung | Zweck |
|------|---------|-----------|---------|
| `/control` | JSON-Text | bidirektional | Registrierung, LEDs, Mikrofon an/aus, Tasten- und Zustandsereignisse, Config-Push, WLAN, Shell-Steuerung |
| `/data` | binär | bidirektional | Mikrofon-PCM hinein; Lautsprecher- und Musik-PCM hinaus |
| `/shell/{device_id}` | roh binär | vom Gerät bei Bedarf gewählt | Root-Shell-Proxy, erst nach einem `shell_open`-Befehl geöffnet |

Alle drei gibt es in einfacher (`ws://`) und TLS-Form (`wss://`); siehe
[Link-Authentifizierung und TLS](#link-authentifizierung-und-tls). Die
`/shell`-Ebene wird nicht gewählt, bevor der Controller darum bittet.

**Keepalive und Verluste.** Der Controller sendet auf jeder Ebene alle 20 s
einen WebSocket-Ping und schließt eine Verbindung nach **30 s** ohne Pong
(`WS_PING_TIMEOUT_S`); beantworte Pings zügig, also blockiere nie die Goroutine
oder den Thread, der den Socket liest. Heim-WLAN verliert Pakete, ein Gerät
sollte auf seinen Sockets also **`TCP_THIN_LINEAR_TIMEOUTS`** setzen (Linux:
auf einem linearen Zeitgeber erneut senden, solange weniger als vier Segmente
unterwegs sind, statt zu verdoppeln); der Controller setzt es auf seiner
Seite. Auf einem Board, dessen Funk sich eine Antenne mit Bluetooth teilt,
**halte jeden BLE-Scan an, solange die Verbindung ein Gespräch, eine Antwort
oder eine Shell-Sitzung trägt** — auf dem Dot führte ein laufender Scan dazu,
dass der Access Point 47–150 % der Frames erneut sendete (siehe „The LE scan
costs the WiFi link" in `device/CLAUDE.md`).

## Registrierung und Fähigkeiten


Direkt nach dem Öffnen des `/control`-Sockets sendet das Gerät eine einzelne
`register`-Nachricht (`device/internal/client/control.go`):

```json
{
  "type": "register",
  "device_id": "<stabile ID>",
  "version": "<Firmware-Version, aus den Build-ldflags>",
  "capabilities": ["mic", "speaker", ...],
  "ip": "<lokale IP, weggelassen bei 127.0.0.1 oder unauflösbar>",
  "ambient_light_status": { "...": "..." },
  "base_os": "emos | fireos | unknown",
  "pairing": true,  // nur während eines Kopplungsfensters (Fähigkeit `pairing`)
  "board": "<pkg/board id, oder unknown>",
  "kernel_arch": "<uname -m, z. B. aarch64>",
  "kernel_release": "<uname -r, z. B. 3.18.19+>",
  "boot_id": "<boot_id des Kernels aus /proc/sys/kernel/random/boot_id>",
  "boot_reason": "<androidboot.bootreason, z. B. power_key, wdt_by_pass_pwk>",
  "emmc": { "rev": 7, "preEol": 1, "lifeA": 1, "lifeB": 2,
            "name": "FJ25AB", "date": "08/2017", "manfid": "0x000015" }

}
```

`base_os`, `board` und die beiden `kernel_*`-Felder beschreiben den Boot und
sind Information: Der Controller speichert und zeigt sie an und schaltet
Android-only-Nutzlasten an `base_os` frei. Das Kernel-Paar entfällt, wenn
`uname` scheitert. Ein Gerät für ein neues Board sollte alle senden.

`boot_id`, `boot_reason` und `emmc` sind Gesundheitswerte des Bootvorgangs;
jeder entfällt, wenn er nicht gelesen werden kann. Der Controller hält für den
Grund eine Zeile je `boot_id` (ein Gerät registriert sich bei jeder
Neuverbindung erneut) und für den Verschleiß eine Zeile je Tag — den schickt
das Gerät auch auf dem `stats`-Takt, alle sechs Stunden neu gelesen, weil ein
Gerät monatelang ohne Neustart laufen kann. Beides steht im Reiter „Status".
`emmc` trägt die EXT_CSD-Bytes des eMMC selbst (JEDEC JESD84-B51): `rev` ist
EXT_CSD_REV [192], und unter 7 bedeuten die anderen drei nichts; `preEol` ist
PRE_EOL_INFO [267] (1 normal, 2 Warnung, 3 dringend); `lifeA`/`lifeB` sind
DEVICE_LIFE_TIME_EST_TYP_A/B [268]/[269] (1–10 in 10-%-Schritten, 11 jenseits
der Auslegung). Finde den eMMC über den Typ
(`/sys/bus/mmc/devices/*/type` = `MMC`), nicht über die Nummer. `boot_reason`
kommt aus der Kernel-Kommandozeile und fehlt, wo diese davor abgeschnitten
ist — wie beim FireOS-6-Kernel auf biscuit.

`capabilities` ist das Aushandlungssignal. Der Dot kündigt die folgenden
bedingungslos an, dazu eine bedingte (`capabilities()` in `control.go`) — die
Liste hier zählt sie bewusst nicht, weil eine Zahl in Prosa veraltet, ohne dass
etwas rot wird:


| Fähigkeit | Bedingung | Bedeutung |
|------------|-----------|---------|
| `mic` | always | Streams mic PCM on `/data` |
| `speaker` | always | Plays speaker PCM from `/data` |
| `leds` | always | Accepts `leds` frames |
| `led_anim` | always | Renders animations locally from one `led_anim` spec per state change (vs. controller-streamed frames) |
| `buttons` | always | Emits `button` events |
| `oww_shadow` | always | Can score the wake word locally and **report** (never act) |
| `oww_trigger` | always | Can **act** on its own wake detection — kept separate from `oww_shadow` on purpose (see below) |
| `button_hold` | always | Emits long-press (`heldMs`) |
| `audio_mix` | always | Holds music on its own frame types and mixes it under voice rather than pausing |
| `oww_local_only` | always | Can listen **privately**: score its own wake word and send nothing until it fires. Whether it is doing so is `listen_state` — see [listening.md](listening.md) |
| `aec_hw_ref` | always | Can take the AEC far-end reference from a playback loopback in the mic capture itself, and falls back to the software tap at the ALSA write when the board has none |
| `output_chain` | always | Can run the speaker output chain (EQ → bass guard → limiter) itself, at the ALSA write, from the config keys `eqBands`, `eqLoudness`, `limiter*`, `bassGuard*`. Runs it only when the controller's `ack` carries `output_chain` too, which is the controller saying it has stopped processing: either half alone keeps the old path, so audio is never shaped twice |
| `wake_cue` | always | Can generate its own wake sound, at `wakeSoundLevel`, independent of volume. Plays it when `wakeSound` is on and a wake has WON: on `listen_ack` for a private-listening session, or on `play_cue` otherwise — never at the crossing, so a ceded wake is silent |
| `volume_cue` | always | Can generate a `volumeButtonSound` preview at the newly selected level after a physical-button change, or repeat it for another Volume Up press at maximum; only while voice and music are idle |
| `wake_mic` | always | Reads `wakeMic` (0 = the centre microphone, 1 to 6 = perimeter MK1 to MK6) and listens for the wake word on that microphone. For a unit whose centre microphone has failed (#705). Without it the controller shows the setting disabled. |
| `remote_volume_arc` | always | Can show the cyan level arc for changed, non-zero remote volume commands when the opt-in `remoteVolumeArc` setting is on. Physical buttons remain unconditional; mute, boot restore, and duplicate state syncs remain silent |
| `response_level` | always | Can boost the voice plane relative to device volume with `responseLevel` (`low` / `medium` / `high` = 0 / +6 / +12dB). The boost is applied before voice/music mixing and capped so it plus device volume never exceeds unity |
| `ambient_light` | only if the sensor is actually readable (`als.Present()`) | Reports light readings |
| `sendspin` | always | Can be a Sendspin player (#89) for synchronised multi-room audio from Music Assistant, run when `sendspinEnabled` is on. Music Assistant connects to the device directly (port 8928, advertised as `_sendspin._tcp`); nothing of the session crosses the controller. Whether it is running, connected or paired is the `sendspin` status |
| `wake_word_off` | always | Honours `wakeWordEnabled: false` (Home Assistant's "No wake word"): a wake crossing is reported as a shadow cross and opens no session and no turn. The button still works. Without it the controller declines "No wake word" for an Echo in `listen_state` `local` |
| `pairing` | always | Asks to pair itself when its owner holds the action button 5 s: a `pair_request` every 5 s on a live link, or otherwise registers with `"pairing": true` on every dial for the 2-minute window, falling back to plain (without its token) when wss cannot connect. The window closes early once new credentials land, so the redial they cause does not ask again. Without it the controller offers the admin a **Pair** action instead, since the device cannot ask |
| `ble_connect` | always | Can hold Bluetooth LE connections for the controller and speak GATT over them (#656): up to three links, requests and results as `0x08` frames on `/data`. Acts only while `bleProxyEnabled` and `bleProxyConnections` are both on, and only against a controller announcing `ble_connect` back |

**`aec_hw_ref` ist eine Fähigkeit mit einem Laufzeitbegleiter, und beide
werden gebraucht.** Die Fähigkeit sagt, dass die Firmware *weiß, wie* man eine
Hardware-Echoreferenz benutzt. Ob das Board tatsächlich eine hat, beantwortet
`aecRef` in der periodischen `stats`-Nachricht — `"hw"`, `"sw"` oder `"off"`,
und **abwesend** bei Firmware, die zu alt ist, um es zu sagen, was nicht als
`"sw"` gelesen werden darf.

Die Trennung gibt es, weil der Nachweis erst zur Laufzeit verfügbar ist: Ein
Kanal ist als Wiedergabe-Loopback bestätigt, wenn er im Leerlauf bitgenau
still ist *und* Ton trägt, während der Lautsprecher spielt — und zur
Registrierungszeit hat nichts gespielt. Wichtig ist das für die
AEC-Verzögerungseinstellung, die beim Software-Abgriff die Latenz vom
Schreiben bis zum Ohr ausgleicht und bei einer bildgenauen Hardwarereferenz
nichts bedeutet. Dieses Bedienelement an die Fähigkeit zu koppeln würde es auf
jedem heutigen Gerät ausgrauen, auch auf denen, die auf den Abgriff
zurückfallen und es brauchen; es an `aecRef == "hw"` zu koppeln deaktiviert es
genau dort, wo es nichts tut.

Ein Board ohne Loopback kündigt `aec_hw_ref` an und meldet dauerhaft
`aecRef: "sw"` — das korrekte abgestufte Verhalten, das keine Änderung am
Controller braucht.

**`oww_shadow` und `oww_trigger` sind zwei Fähigkeiten, nicht eine, und diese
Trennung ist tragend.** Shadow kam zuerst, es gibt also Firmware im Feld, die
bewertet und meldet, aber keinen Code hat, um auf eine Erkennung zu handeln.
Ein Controller, der „kann bewerten" als „kann auslösen" läse, würde seine
eigene Wake-Erkennung zurückstellen und auf einen Auslöser warten, den das
Gerät nie sendet — ein Gerät, das perfekt bewertet und nie antwortet. Ein
Board, das lokal bewertet, SOLLTE beide ankündigen; ein Board, das nur
Bewertungen weiterreicht, kündigt allein `oww_shadow` an.

Der Controller liest Fähigkeiten über `@property`-Zugänge an der Klasse
`Device` (`controller/em_controller.py`): `led_anim_capable`,
`audio_mix_capable`, `button_hold_capable`, `oww_shadow_capable`,
`oww_trigger_capable`, `aec_hw_ref_capable`, dazu direkte
Mitgliedschaftsprüfungen für die vier Grundfähigkeiten
(`mic`/`speaker`/`leds`/`buttons`). Jede Zeichenkette, die der Controller
prüft, muss eine sein, die das Gerät senden kann, und jede Zeichenkette, die
das Gerät sendet, muss eine sein, die der Controller versteht — beide
Richtungen sind von `test_capabilities.py` festgenagelt, denn ein Tippfehler
auf einer der beiden Seiten scheitert stillschweigend.

## `/control` — JSON-Nachrichten

Auf dem Gerät vom Typ-Switch in `control.go` verteilt. Unbekannte Typen werden
ignoriert (Vorwärtskompatibilität). Die unten genannten Nutzlastfelder sind
die, auf die reagiert wird; abwesende optionale Felder behalten das vorherige
bzw. voreingestellte Verhalten.

**Gerät → Controller**

| `type` | Nutzlast | Bedeutung |
|--------|---------|---------|
| `register` | see above | Handshake, sent once at connect |
| `button` | `clickType`, `down`, `heldMs`, `muted`, `button.type` | Button press/release; `heldMs` only if `button_hold` |
| `mute_state` | `muted` | Mute toggled (mute is device-sovereign — see `device/CLAUDE.md`) |
| `volume_state` | `level` | Volume changed; controller persists it as `startupVolume` |
| `oww_shadow_cross` | score/threshold/age fields | Shadow-mode wake crossing (report only) |
| `oww_wake` | `score`, `threshold`, `ageMs`, `capturedMono`; `level`, `peak` when the crossing frame is still buffered; under private listening also `session`, `floor`, `barge` | On-device trigger fired (`owwOnDevice=on`). With `session` it opened a private-listening session whose audio follows as `0x07` ([listening.md](listening.md)); without, it lands in `Device.pending_wake` and the continuous stream carries the audio. `level`/`peak` are the wake word's loudness in dBFS with `micGainDb` removed, over the 2.0s ending at the crossing frame (definition: `controller/em_wakelevel.py`); logged, not acted on |
| `listen_state` | `state` (`local`/`stream`/`degraded`), `reason?` | What the device is doing with its wake stream. Sent on every change and after every `ack` |
| `listen_end` | `session`, `reason` | The device closed a session itself (`ack_timeout`, `max_open`, `muted`, `link`, `stopped`) |
| `ambient_light` | `value` | Light reading (only if `ambient_light`) |
| `stats` | hardware and link telemetry (`internal/client/stats.go`, `DeviceStats`) | Every ~30s, plus once on connect. Every field is optional and absence means **not measured**, never zero. `tcpUpRetrans` is the device's own TCP retransmits across its planes since the last report, `tcpUpSegs` the segments sent where the kernel counts them (FireOS 5's 3.18 does not); `ble` carries the scanner's counters, including `yields`/`yieldedMs` for time the scan stood aside for the link |
| `ble_adverts` | `adverts[]` | Batch from the passive BLE scanner. **Legacy path** — send these on `/data` as `0x06` whenever the controller announced `ble_adverts_data`, and use this message only when it did not (#404) |
| `wifi_scan_result` | `networks[]` of `{ssid, ssid_hex, signal}`, or `error` | Answer to `wifi_scan` |
| `wifi_result` | `ok`, `ssid`, `error?` | Outcome of a `wifi_change`, re-sent until `wifi_commit` |
| `pair_request` | — | The owner held the action button on a connected device (only if `pairing`). The controller shows **Approve pairing**; approval issues a rotated token and the CA over the shell plane, then bounces the link |
| `sendspin_status` | `status` (`sendspin.Status`), or null when the player stopped | The Sendspin player's state as it changes: `state` (`listening`, `connected`, `playing`, `busy` while Home Assistant holds the music plane, `error`), `server`, `paired`, `pairedWith`, `group`, `synced`, `syncErrUs`, `bufferedMs`, player counters. No secrets. The `stats` report carries the same object as `sendspin` |
| `sendspin_token` | `token`, `clientId`, or `error` | Answer to `sendspin_token_request`. The token (`SP:0…`) carries the device's Sendspin pairing key: the controller hands it to the one waiting request and never logs or stores it |
| `pong` | `id`, `mono` when answering a `ping` that carried an `id` | Keepalive reply. `id` echoes the ping's; `mono` is the device's monotonic clock in ms (any fixed origin), which the controller maps onto its own to date `capturedMono`. Unsolicited keepalive pongs carry neither |

**Controller → Gerät**

| `type` | Nutzlast | Bedeutung |
|--------|---------|---------|
| `ack` | `device_id`, `features[]` | Registration accepted. `features` is the CONTROLLER's capability list — the mirror of the device's own, and read the same way: a feature that is absent is one the controller cannot do. Absent entirely on controllers before 2.23.0. Current: `ble_adverts_data`, `listen_session`, `output_chain`, `ble_connect` |
| `refused` | — | Not admitted: link auth refused this device's credentials (a wrong token, or a token it has stopped presenting). Sent before the close; firmware with `pairing` shows the refused ring, which tells the owner to hold the action button to pair. A device also treats a TLS certificate its CA did not sign as refused. Older firmware ignores it |
| `pending` | `pairing?` | Not admitted: the device is unapproved, or with `pairing:true` its pairing request is recorded and waiting for an admin. Keep redialling within the window; an approval admits the next dial |
| `leds` | `leds[]`, `listening?` | One LED frame; `listening:true` marks the listening ring so the direction overlay keys off it |
| `led_anim` | `{pattern, colors, periodMs, ttlSec}` | Local animation spec; sent only if `led_anim` |
| `mic_start` | `lock_mic?` | Start mic stream. `lock_mic:false`/absent = always-on ungated wake stream; `true` = bounded, VAD-gated turn |
| `mic_stop` | — | Stop the mic stream. Under private listening it ends a bounded turn but **not** a session (only `listen_close` does, by id) and **not** local listening, which is what hears a barge-in |
| `listen_ack` | `session` | A private-listening wake was taken; stops the device's 3s ack clock |
| `listen_close` | `session`, `reason` | End that session. Ignored if it is not the open one |
| `beam_lock` / `beam_unlock` | — | Lock beamformer to the chosen perimeter mic for a turn / return to omni |
| `volume_set` | `level` | Set absolute volume |
| `duck` | `on` | Duck music under a voice turn (turn start/end) |
| `config` | `ConfigMessage` fields | Push configuration (see below) |
| `wifi_scan` | — | Scan for networks; answered with `wifi_scan_result` |
| `wifi_change` / `wifi_commit` | `ssid`, `ssid_hex?`, `psk` / — | Switch WiFi with auto-rollback; commit finalises |
| `shell_open` / `shell_close` | `pty?` | Ask the device to dial `/shell` (`pty:true` = interactive) / close it |
| `play_cue` | `cue` | Play a cue the device generates itself. Only `"wake"` today, sent when `wakeSound` is on and a wake outside a private-listening session has won arbitration; unknown names are ignored |
| `music_flush` / `speaker_flush` | — | Flush the music / voice buffer (barge-in uses `speaker_flush`) |
| `sendspin_token_request` | — | Ask for the Sendspin pairing token, for the dashboard to show on an admin's request; answered with `sendspin_token` |

**Eine SSID sind 0–32 beliebige Bytes**, ein Name allein kann ein Netz also
nicht immer adressieren. `ssid` ist zur Anzeige da (ungültiges UTF-8 wird als
U+FFFD gezeigt); `ssid_hex` sind die exakten Bytes, in jedem Scan-Ergebnis
gemeldet und bei `wifi_change` zurückgeschickt, wenn das Netz aus einem Scan
stammt. Ein `wifi_change` ohne dieses Feld meint das UTF-8 von `ssid`. `psk`
ist leer bei einem offenen Netz, 8–63 druckbare ASCII-Zeichen oder ein rohes
64-stelliges Hex-PSK. Firmware, die älter ist als `ssid_hex`, ignoriert es.

## `/data` — binäre Frames

Das erste Byte ist der Frame-Typ, der Rest die Nutzlast. **Die Typcodes sind
nach Richtung getrennt** — `0x04`/`0x05` bedeuten Unterschiedliches, je
nachdem, wer sie gesendet hat, und das ist Absicht
(`device/internal/client/data.go:25`). Wer ein Board umsetzt, darf keine
einzelne globale Tabelle lesen.

**Controller → Gerät (Wiedergabe)**

| Code | Name | Bedeutung |
|------|------|---------|
| `0x02` | speaker | Voice-response PCM chunk; played immediately |
| `0x03` | speaker EOS | End of voice stream |
| `0x04` | music | Music PCM chunk on its own plane — **only if `audio_mix`** |
| `0x05` | music EOS | End of music stream — **only if `audio_mix`** |
| `0x08` | ble gatt | One JSON request of the Bluetooth connection bridge — **only if the device announced `ble_connect`** |

**Gerät → Controller (Aufnahme)**

| Code | Name | Bedeutung |
|------|------|---------|
| `0x01` | mic | Mic PCM chunk (mono `S16_LE`, 16 kHz) |
| `0x04` | VAD-end | Bounded turn: speech was detected, then ended |
| `0x05` | no-speech-timeout | Bounded turn: no speech ever detected before the timeout |
| `0x06` | ble adverts | Batch of scanned BLE advertisements — **only if the controller announced `ble_adverts_data`** |
| `0x07` | session audio | `[0x07][session u32 BE][seq u16 BE][PCM]` — private-listening audio, **only if the controller announced `listen_session`** |
| `0x08` | ble gatt | One JSON result or event of the Bluetooth connection bridge — **only if the controller announced `ble_connect`** |

`0x04`/`0x05` lassen sich gefahrlos doppelt verwenden, weil Wiedergabe-Frames
nur zum Gerät und Aufnahme-Frames nur von ihm fließen. Die beiden
Aufnahme-Endmarken sind bewusst unterschiedlich: „Wakeword, dann Stille"
(leise aufgeben) muss von „gesprochen, und das Backend hatte nichts zu sagen"
unterscheidbar sein.

Formen des Mikrofonstroms (`device/CLAUDE.md`, Device audio pipeline):

- Der **immer laufende Wake-Strom** (`mic_start` ohne `lock_mic`) ist
  ungegattert und AGC-frei — jede Periode wird durchgehend gesendet, damit die
  Wake-Bewertung einen ununterbrochenen Strom sieht.
- Der **begrenzte Gesprächsstrom** (`lock_mic:true`) ist VAD-gegattert mit
  einem Vorlaufring, endet mit einer `0x04`-Endmarke, wenn das Gatter nach
  Sprache schließt, und mit `0x05`, wenn innerhalb des Timeouts keine Sprache
  kam.
- **Privates Zuhören** (`owwOnDevice=on`, Gerät kündigt `oww_local_only` an,
  Controller kündigt `listen_session` an): Der immer laufende Strom läuft
  weiter, aber nur auf dem Gerät — er speist die lokale Bewertung und sendet
  nichts. Ein Wecken eröffnet eine Sitzung, und ihr Ton geht als `0x07` hinauf,
  bis der Controller sie am Ende der Sprache schließt oder eine Grenze auf dem
  Gerät es tut. Der vollständige Vertrag ist [listening.md](listening.md); ein
  neues Board, das lokal bewertet, sollte ihn umsetzen statt zu senden.

### `0x06` — BLE-Advertisements

Die Nutzlast ist UTF-8-JSON, `{"adverts": [ ... ]}`, jeder Eintrag wie oben
unter `ble_adverts` beschrieben. Sie trägt keinen Sequenzkopf: Ein Stapel ist
in sich abgeschlossen, und ein verlorener ist keine erneute Übertragung wert.

**Sende diese nur, wenn der Controller in seinem `ack` `ble_adverts_data`
angekündigt hat.** Ein Controller, der das nicht tat, ignoriert das Frame —
unbekannte Frame-Typen werden in beide Richtungen ignoriert — und jedes
Advertisement verschwindet, ohne dass irgendwo ein Fehler auftaucht. Weich
stattdessen auf die Kontrollnachricht `ble_adverts` aus.

Zwei Regeln für die Senderseite, von denen der Controller keine für dich
erzwingen kann:

- **Sende nicht, während ein BEGRENZTES GESPRÄCH streamt** (`mic_start` mit
  `lock_mic:true`). Die Datenebene ist ein einziger TCP-Strom, ein bereits
  geschriebenes Frame lässt sich also nicht vordrängeln; der einzige Weg,
  dass der Ton eines Gesprächs nicht hinter Telemetrie ansteht, ist, die
  Telemetrie nicht zu schreiben. Ein Gespräch dauert Sekunden, und Home
  Assistant duldet 195 s Veralterung pro Gerät — nachgelagert merkt das also
  nichts.

  **Dehne das nicht auf den immer laufenden Wake-Strom aus.** Der läuft auf
  jedem Gerät immer, dessen Wakeword controllerseitig bewertet wird;
  Advertisements zurückzuhalten, sobald das Mikrofon streamt, verwirft also
  für immer jeden Stapel, und der Proxy stirbt ohne Fehler an beiden Enden.
- **Verwirf einen Stapel, den du nicht senden kannst, statt auf die
  Kontrollebene auszuweichen.** Auszuweichen legt Massentelemetrie genau dann
  auf den Lebendigkeitskanal, wenn die Verbindung ohnehin in Schwierigkeiten
  ist — und genau dafür existiert dieses Frame.

### `0x08` — Bluetooth connections (GATT)

The one frame code used in both directions. The payload is one UTF-8 JSON
object. The device is a GATT **client**: it connects out to peripherals for
Home Assistant, and holds at most three links (`limit` below says how many).

**Negotiated both ways.** The device sends nothing unless the controller's
`ack` carried `ble_connect`; the controller sends nothing unless the device
announced it. Connections also need `bleProxyEnabled` and
`bleProxyConnections` on — the links live inside the scan session.

Controller → device. Every request carries a `req` number the result echoes.
Addresses are `aa:bb:cc:dd:ee:ff`; `addr_type` is 0 public, 1 random.

| `t` | Fields | Result adds |
|-----|--------|-------------|
| `connect` | `addr`, `addr_type` | `mtu` |
| `disconnect` | `addr` | — (already gone is `ok`) |
| `services` | `addr` | `services[]`: `uuid`, `start`, `end`, `chars[]`: `uuid`, `handle` (declaration), `value_handle`, `props`, `descs[]`: `uuid`, `handle` |
| `read` | `addr`, `handle` | `value` (base64; a long value is followed to its end) |
| `write` | `addr`, `handle`, `value` (base64), `response` | — |
| `slots` | — | answered by a `slots` event carrying the `req` |

Device → controller.

| `t` | Fields | Meaning |
|-----|--------|---------|
| `result` | `req`, `ok`, and on failure `error`, `att?`, `detail?` | The answer to one request |
| `notify` | `addr`, `handle`, `value` (base64), `ind` | A notification, or an indication (`ind:true`, already confirmed to the peer) |
| `disconnected` | `addr`, `reason` | A link ended, whoever ended it. `reason` is the HCI code (`0x08` supervision timeout, `0x13` the peer, `0x16` us) |
| `slots` | `free`, `limit`, `addrs[]` | Sent whenever it changes. `free` is 0 while connections are switched off |

`error` is one of: `disabled` (the setting is off), `no_slots`, `timeout` (the
peer did not answer a connect within 20s), `already_connected`,
`not_connected`, `disconnected` (the link dropped mid-request), `att` (the
peer refused; `att` carries its ATT error code, e.g. 5 for insufficient
authentication), `att_timeout` (no answer in 30s, after which the link is
dropped, as the ATT specification requires), `too_long` (a write over
MTU−3 bytes), `not_running` (the scan session is down), `busy`, `bad_request`,
`failed` (`detail` says what, for a log).

Rules a device must keep:

- **Requests for one peer run in the order they arrived.** Home Assistant
  writes a command and then reads its result. Different peers must not wait
  for each other: a connect can take 20s.
- **UUIDs are the full 128-bit form**, lowercase with hyphens, whatever
  length the peer used.
- **Uuids, handles and values are the peer's own and are passed on as given.**
  The device does not subscribe on the controller's behalf: enabling a
  notification is a `write` of the CCCD descriptor like any other, and every
  notification that arrives is forwarded.
- **There is no pairing.** A peer's Security Request is answered with Pairing
  Not Supported, and attributes that need it fail with `att` 5.
- **Drop every link when the control connection goes.** A result has nowhere
  to go, and a peripheral's connection slot is better freed than held.
- **Do not hold these frames back for a turn**, unlike `0x06`. A result is
  owed to a request.
- A link is made at a 30ms connection interval and moved to 500ms after 5s
  without a request, because the interval decides how much of the scan
  survives beside it (24% of adverts at 30ms, about 56% at 500ms, measured on
  biscuit). A board with separate radios need not do this.

## Config push — `ConfigMessage`

Der Controller sendet `config` beim Verbinden und bei jeder Änderung der
Gerätekonfiguration. Felder sind camelCase
(`device/internal/config/config.go`). **Teilaktualisierung: Felder ungleich
null werden angewandt, Felder gleich null bzw. nil werden ignoriert** — daher
gibt es Zeigertypen (`*bool`, `*int`), wo `false`/`0` von „nicht gesetzt"
unterscheidbar sein muss. Änderungen wirken sofort, ohne Neustart.

Der kanonische Feldsatz (`CLAUDE.md` im Repository-Wurzelverzeichnis, „Device
config push"):

```
vadThreshold, vadSpeechMs, vadSilenceMs,
owwThreshold, owwModel, owwSpeexNs, owwOnDevice,
adcDigitalGain, adcMicpga, micGainDb,
startupVolume,
beamAngle, beamformingEnabled,
aecEnabled, aecDelayMs, aecTailMs, agcEnabled, nsAsr,
bargeInEnabled, bargeInThreshold,
bleProxyEnabled, bleProxyConnections,
sendspinEnabled, sendspinUnpaired, sendspinName,
eqBands, eqLoudness, limiterEnabled, limiterThreshold, limiterRelease,
bassGuardEnabled, bassGuardDb,
ledScene, ledListenColor, ledThinkColor,
meterAttack, meterDecay, meterFloor, meterGamma, meterRef, meterCurve,
wakeArbitrationMs, duckDb, responseLevel,
buttonSingleTapEvent, buttonMultiTapMs,
owwOnDevice, saveUtterances, streamReply,
wakeSound, wakeSoundLevel, volumeButtonSound
```

`wakeWordEnabled` is sent on its own, not with the stored config: it is
Home Assistant's per-Echo picker state, pushed on connect and on change to
firmware announcing `wake_word_off`. A pointer, because false is the value
that matters.

Not every field is acted on by the device. The output-chain keys (`limiter*`,
`bassGuard*`), `eq*`, `saveUtterances`, `streamReply`, `wakeArbitrationMs`, and the
`button*` timing keys are **controller-side** — that processing happens before the audio
reaches the wire, or is used only for config scoping. `owwOnDevice` is both
controller-consumed (scoping) and device-acted. `sendspinName` is not a
stored setting: it is the device's label, added to the push at registration
and sent alone on a rename, because Music Assistant lists the player by it. A
new board only needs to
implement the keys relevant to hardware it actually has; unknown keys are
ignored, which is the correct degrade.

## Link-Authentifizierung und TLS

- Alle drei Ebenen tragen einen `X-EM-Token`-Header, bei **jedem Wählvorgang**
  aus der Zugangsdatei des Geräts gelesen
  (`device/internal/client/tlscreds.go`) — eine aufgespielte Zugangsberechtigung
  wirkt also beim nächsten Verbinden, ohne Neustart. Ein Gerät, das eine
  Zertifizierungsstelle hält, sendet seinen Token **nie** über eine
  unverschlüsselte Verbindung.
- Ein Gerät mit einer Zertifizierungsstelle auf der Platte wählt **nur**
  `wss://`, auf dem `tls_port` aus dem mDNS-TXT-Eintrag oder seiner
  Endpunktdatei; ohne `tls_port` wählt es gar nicht. Die eine Ausnahme ist ein
  Kopplungsfenster (`pairing`, siehe oben): erst wss, dann unverschlüsselt
  ohne Token, beide mit `"pairing": true` in der Registrierung. Ein Gerät ohne
  Zertifizierungsstelle wählt unverschlüsselt. Die Serveridentität ist der
  feste DNS-SAN `revoice-controller`, nie eine IP.
- Zertifikate sind rückdatiert und langlebig, **und** das Gerät klemmt seine
  Prüfuhr auf die Bauzeit der Firmware, weil ein Echo vor NTP mit falscher Uhr
  startet und ein Gerät, das sich nicht verbinden kann, seine Uhr nicht
  richten kann. Ein neues Board erbt das — „normalisiere" keine der beiden
  Hälften.
- Durchgesetzt wird das von `em_linkauth.decide`
  (`controller/em_linkauth.py`): Ein falscher Token wird immer abgewiesen; ein
  gespeicherter Token ohne vorgelegten wird zugelassen, **aber nur, bis das
  Gerät ihn einmal vorgelegt hat** (der Zugangs-Push selbst reitet auf der
  unverschlüsselten Ebene), danach abgewiesen; ein Token für ein Gerät, zu dem
  nichts gespeichert ist, wird ignoriert, nicht abgewiesen.
  `REQUIRE_DEVICE_TLS=1` macht TLS und Token verpflichtend. **Ein
  Geräte-Binary muss seinen Token auf allen drei Ebenen senden**, sonst wird
  es auf denen abgewiesen, auf denen er fehlt, sobald der Controller ihn
  einmal gesehen hat.
- `/data` und `/shell` werden nur von der Adresse und mit dem Schema der
  lebenden `/control`-Verbindung des Geräts zugelassen
  (`em_linkauth.follows_control`) — ein echtes Gerät muss also alle drei von
  einer Adresse aus wählen.


## Was dem Geräte-Binary gehört

Für jedes Board ist das Geräte-Binary der lokale Hardware-Agent eines
Sprachsatelliten. Seine Aufgaben:

- **Aufnahme für den Sprachassistenten** — Mikrofon-Streaming (Wake-Strom und
  begrenzte Gespräche), die Aufnahmepipeline auf dem Gerät
  (Beamforming/Verstärkung/AEC/AGC/VAD, soweit die Hardware es hergibt) und
  optional Wake-Bewertung und -Auslösung auf dem Gerät.
- **Wiedergabe** — Lautsprecherausgabe, und Musik auf eigener Ebene, unter
  Sprache gemischt (Ducking), wenn `audio_mix` angekündigt wird.
- **Tasten** — Ereignisse für Drücken, Halten und Mute, aufgelöst über den
  **Namen** des Geräts, nicht über den Index (`event2` ist auf einem Board
  eine Taste und auf einem anderen ein Touchscreen).
- **Durchsagen** — vom Controller geschobenen Ton außerhalb eines Gesprächs
  abspielen.
- **Statusausgabe** — LEDs beim Dot; auf einem Board ohne eigenen oder mit
  geliehenem Bildschirm die minimale Statusfläche, die dieses Board festlegt
  (oder keine).

Alles, was ein Backend vernünftigerweise erledigen kann — Absichten, TTS,
HA-Entitäten —, bleibt im Controller bzw. in Home Assistant über die
ESPHome-Voice-Assistant-API. Das Gerät setzt das nicht neu um und hängt für
seine eigene Arbeit nicht von Amazons HALs oder Bibliotheken ab (siehe den
Richtungshinweis in `CLAUDE.md` im Wurzelverzeichnis).

Die `crown`-Bindings werden hinter einem Go-Build-Tag gebaut, das `server`
spiegelt (ADR-0001). Die Hardware-Inventur — ALSA-Karten und -Geräte, Formate,
Pfade der Eingabegeräte, Autostart — steht in
`docs/echo-show-8-hardware-map.md` auf dem Branch von
[PR #358](https://github.com/wilbowes/EchoMuse/pull/358), der nicht gemergt ist; dieser Abschnitt hält nur fest, worauf
sich die *Schnittstelle* für das MVP festlegt.

Die `crown`-Bindings werden hinter einem Go-Build-Tag gebaut, das `server`
spiegelt (ADR-0001). Das Hardware-Inventar — ALSA-Karten und -Geräte,
Formate, Pfade der Eingabegeräte, Autostart — steht in
[echo-show-8-hardware-map.md](echo-show-8-hardware-map.md); dieser Abschnitt
hält nur fest, worauf sich die *Schnittstelle* für das MVP festlegt.

**Fähigkeiten fürs MVP** (Teilmenge der des Dots):

| Fähigkeit | crown-MVP | Anmerkung |
|------------|-----------|------|
| `speaker` | ja | `card0,device0` → RT5616 (Issue #5). Streamt sauber, aber `Ext_Speaker_Amp_Switch` ist invertiert (`On` = still, `Off` = hörbar) — dieselbe Falle wie bei checkers, am 2026-08-26 nach Gehör bestätigt. Muss in der Init des Bindings auf `Off` gefahren werden; die Voreinstellung beim Start ist `On` |
| `mic` | **nachgewiesen** | `card0,device22`, 6 Kanäle / 16 kHz / `S24_3LE`, am 2026-08-26 durch Aufnahme auf echter Hardware bestätigt — echtes Signal, keine digitalen Nullen. HW_REFINE deckt sich exakt mit den Treiberkonstanten von checkers. Offen: Störabstand im ruhigen Raum und über Distanz, nicht die Rohaufnahme — siehe unten |
| `buttons` | ja | Über den Namen aufgelöst (`gpio-keys` Lautstärke, Aktionstaste, Kameraverschluss) |
| `leds` / `led_anim` | **nein** | Kein LED-Ring; eine Statusüberlagerung für „Sprachgespräch" auf dem Display ist auf nach dem MVP verschoben und bleibt auch dann dem Nutzer aus dem Weg |
| `audio_mix` | später | Für die MVP-Sprachschleife nicht nötig |
| `ambient_light` | offen | Nur wenn ein lesbarer Sensor gefunden wird |
| `oww_shadow` / `oww_trigger` | nein (MVP) | Das MVP nutzt das Wakeword **controllerseitig** |

**Audio-Besitz** (ADR-0002): `crown` beansprucht Mikrofon und Lautsprecher
exklusiv, solange Revoice läuft, genau wie der Dot. Das Mikrofon wird
**durchgehend gehalten** — das Wakeword liegt im MVP beim Controller, das Gerät
streamt Mikrofon-PCM also die ganze Zeit, sonst wird es taub für das nächste
Wakeword. Der **Lautsprecher** wird für ein Gespräch geholt (die Antwort und
alle Medien, die das Gespräch verlangte) und danach freigegeben — das
Alexa-Modell. Der Bildschirm gehört durchgehend vollständig der eigenen
Software der Nutzerin.

**Das Mikrofon war der MVP-Blocker; das ist es nicht mehr.** Die Mikrofone
sitzen auf externen TLV320AIC3101-Dies (TDM, `card0,device22`, 6 Kanäle /
16 kHz / `S24_3LE`) — dasselbe Format, das der Dot bereits aufnimmt
(`device/internal/bindings/mic/pcm_microphone.go`, 9 Kanäle / 16000 Hz /
`tinyalsa.PCM_FORMAT_S24_3LE`), nur mit weniger Kanälen, gleicher
`Channels * 3`-Schrittweite, gleichem `DeviceConfig`-gesteuerten Pfad
danach. Am 2026-08-26 auf echter Hardware mit `device/tools/capture_mics`
nachgewiesen: öffnet sauber, echtes Signal auf allen 4 Mikrofonkanälen, keine
digitalen Nullen. `device/tools/hw_refine_probe` bestätigt, dass der Bereich
des Treibers exakt den Konstanten von checkers entspricht. Die vollständige
Geschichte (warum „digitale Nullen" die ursprünglich falsche Lesart war, die
Untersuchung zur Verstärkung, die tatsächlichen Aufnahmedaten) steht in
derselben Datei auf dem Branch von [PR #358](https://github.com/wilbowes/EchoMuse/pull/358).

**Weiterhin offen: der Störabstand im ruhigen Raum und über Distanz**, nicht
die Rohaufnahme — der Test von heute war eine Plausibilitätsprüfung in einem
lauten Raum mit laufender Musik, keine Messung der Wake-Zuverlässigkeit.
Lautsprecher (#5) und Mikrofon (#6) sind beide nachgewiesen; der Störabstand
ist das verbleibende Ja-oder-Nein für den MVP-Meilenstein (Start → verbinden →
HA → Wakeword → Assist → gesprochene Antwort).

Gilt für jedes crown-Binding, nicht nur fürs Mikrofon: Eingabegeräte und
i2c-Adressen **über den Namen** auflösen, nie über die Nummer — die
`eventN`-Nummerierung ist über Boards hinweg nicht stabil (auf `biscuit` ist
es die Lautstärketaste, auf `checkers` der Touchscreen), und ein Öffnen mit
falscher Nummer scheitert still statt mit einem Fehler. Siehe auch Issue #36
(Echo Show 5 / `checkers`) — dieselbe Form von Inbetriebnahmeproblem, lesenswert,
bevor man crown-spezifische Bindings schreibt.

## Invarianten (was die Tests festnageln)

`controller/tests/test_capabilities.py` ist die Leitplanke dieses Vertrags:

- Das Gerät kündigt die erwarteten Grundfähigkeiten an.
- Jede Fähigkeitszeichenkette, die der Controller prüft, ist eine, die das
  Gerät senden kann, und umgekehrt (beidseitiger Tippfehlerschutz).
- `oww_shadow` und `oww_trigger` bleiben getrennt.
- Fähigkeiten, die gemeldet werden, bevor der ESPHome-Server existiert, gehen
  nicht verloren.
- Eine Änderung der Fähigkeiten stößt die HA-Verbindung an, damit sich die
  Entitätsliste erneuert.
- Ein deaktiviertes Bedienelement schreibt nicht stillschweigend, wenn seine
  Fähigkeit fehlt.
- Privates Zuhören wird in beide Richtungen ausgehandelt: `oww_local_only`
  vom Gerät, `listen_session` im `ack`, mit denselben Zeichenketten auf beiden
  Seiten.

Die Sitzungsregeln selbst — die Fristen auf dem Gerät, der Ring, welche
Sitzung ein Frame erreichen darf — werden von `device/internal/listen` und
`controller/tests/test_listen.py` festgehalten.

Eine Board- oder Protokolländerung, die eine davon bricht, muss den Test mit
Begründung anpassen, statt ihn zu umgehen.
