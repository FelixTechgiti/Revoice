# Zuhören: wer entscheidet, und wann Ton den Echo verlässt

Das ist die Spezifikation dafür, wie ein Echo auf sein Wakeword horcht und
wann sein Mikrofonton das Gerät verlässt. Sie ist die Referenz für die
Firmware (`device/internal/listen/`), den Controller
(`controller/em_listen.py`), das Protokoll auf der Leitung
(`docs/device-controller-interface.md`) und jede Aussage über Privatsphäre,
die ein Nutzer zu sehen bekommt. Widersprechen sich Code und dieses Dokument,
ist eines von beiden ein Fehler.

## Zwei Modi, pro Echo gewählt

| Modus | Gespeicherter Wert (`owwOnDevice`) | Wakeword läuft auf | Ton verlässt den Echo |
|-------|------------------------------------|--------------------|------------------------|
| **Auf diesem Echo** (Voreinstellung) | `on` | dem Echo | nur, während du mit ihm sprichst |
| **Auf dem Controller** | `off` | dem Controller | **dauerhaft**, an den Controller in deinem LAN |

Einen dritten Modus für Nutzer gibt es nicht. `shadow` (beide Erkennungen
bewerten denselben durchgehenden Strom, um sie zu vergleichen) existiert
weiterhin als **Diagnosewerkzeug für die Entwicklung**: Es wird im Dashboard
nicht angeboten und über die Konfigurations-API gesetzt. Ein Echo, der schon
darin steht, zeigt es an, als sendend gekennzeichnet — denn das ist er.

Jeder Echo ist in genau einem Modus. Eine Flotte darf sie mischen, und die
Arbitrierung funktioniert über die Mischung hinweg (siehe *Arbitrierung*). Das
Dashboard zeigt den Modus jedes Echos, sagt klar, wenn ein Echo sendet, und
fasst die Flotte zusammen („1 von 3 Echos sendet dauerhaft").

Die gespeicherten Werte stammen aus dem ursprünglichen Entwurf mit drei Modi
(`off`/`shadow`/`on`), damit bestehende Konfiguration und Firmware ihre
Bedeutung behalten; geändert hat sich nur die Darstellung.

## Was „Auf diesem Echo" genau heißt

**Kein Ton verlässt den Echo, bevor seine eigene Wakeword-Erkennung feuert.**
Danach sendet er den Ton, der auf das Wakeword folgt, bis eines davon
eintritt:

1. Der Controller sagt, die Äußerung sei zu Ende (Home Assistants Satzende,
   oder der eigene Endpunkt des Controllers, wenn HAs nie anspringt).
2. Der Controller lehnt das Wecken ab (ein anderer Echo hat die Arbitrierung
   gewonnen, es gibt keine Home-Assistant-Verbindung, oder ein Gespräch läuft
   schon und Barge-in ist aus).
3. Der Controller hat das Wecken nicht innerhalb von `ackTimeout` (3 s)
   bestätigt.
4. Die Sitzung ist seit `maxOpen` (30 s) offen.
5. Der Echo ist stummgeschaltet, oder seine Datenverbindung bricht ab.

Die Regeln 3 bis 5 setzt **der Echo selbst** durch, eine verlorene Nachricht,
ein abgestürzter Controller oder eine getrennte Netzwerkhälfte können ihn also
nicht sendend zurücklassen.

Während eine Antwort läuft, hört der Echo **lokal** weiter. Das Wakeword über
die Antwort zu sagen (Barge-in) wird auf dem Echo erkannt, der dann eine neue
Sitzung eröffnet, genau wie oben. Während der Antwort wird nichts gesendet,
solange das Wakeword nicht fällt.

Nur zwei Dinge öffnen das Mikrofon ohne ein lokales Wecken, und bei beiden
bittet der Nutzer ums Sprechen oder wird darum gebeten: die **Aktionstaste**
und eine **Nachfrage** von Home Assistant („continue conversation", oder eine
Aktion `ask_question` / `start_conversation`). Beide nutzen den begrenzten
Gesprächsstrom (`mic_start` mit `lock_mic:true`), den der Echo auf Sprache
gattert und nach 5 s ohne etwas selbst beendet; der Controller beendet ihn am
Satzende, genau wie er eine Sitzung schließt, und der Echo geht zurück ins
lokale Zuhören.

### Was ehrlich zu behaupten ist, und was nicht

- Sag: *Ton verlässt den Echo erst, nachdem er das Wakeword gehört hat, und
  nur, bis du zu Ende gesprochen hast.*
- Sag: *ein Fehlauslöser schickt ein paar Sekunden Ton, die du nicht gemeint
  hast.* Das gilt für jedes Wakeword-System, Amazons eingeschlossen, und wir
  schreiben es hin, statt uns darauf ertappen zu lassen.
- Sag **nicht** *Ton verlässt das Gerät nie* oder *vollständig lokal*: Der
  Befehl geht an den Controller und weiter an Home Assistants
  Spracherkennung, wo auch immer der Nutzer sie eingerichtet hat.

## Zustände, in denen ein Echo sein kann

Der Controller löst pro Echo einen davon auf (`em_listen.resolve`), und das
Dashboard zeigt ihn. Die Konfiguration sagt, was gewünscht war; das hier sagt,
was wahr ist.

| Zustand | Bedeutung | Sendet dauerhaft? |
|---------|-----------|-------------------|
| `local` | Wakeword auf dem Echo, hört zu | nein |
| `controller` | Wakeword auf dem Controller, laut Konfiguration | **ja** |
| `diagnostic` | `shadow`, beide Erkennungen an | **ja** |
| `legacy` | „Auf diesem Echo" gewünscht, aber die Firmware ist älter als `oww_local_only` | **ja** — sagt „Firmware aktualisieren für privates Zuhören" |
| `degraded` | „Auf diesem Echo" gewünscht, aber der Echo kann nicht bewerten (Laufzeit oder Modell fehlt, oder ließ sich nicht laden) | nein — **nur per Taste**, mitsamt Grund |
| `unknown` | Der Echo hat noch nichts gemeldet | wird als unbekannt gezeigt, nie als privat |

**`degraded` fällt nie aufs Senden zurück.** Ein Gerät, das sein eigenes
Wakeword nicht ausführen kann, ist entweder kaputt oder wartet auf eine
Installation, und es beantwortet weiterhin die Taste. Still stattdessen zu
senden machte die Aussage des Dashboards über Privatsphäre falsch, ohne dass
irgendwer das gewählt hätte.

`legacy` sendet tatsächlich und wird dabei auch so gezeigt. Das ist das
Verhalten heutiger Firmware; die Behebung ist ein Firmware-Update, und das
Dashboard sagt es.

Der Zustand kommt vom Echo selbst (`listen_state` auf `/control`), nie aus
der Lesart der Konfiguration durch den Controller: Der Echo ist der einzige
Beteiligte, der weiß, ob seine Bewertung geladen hat. `oww_local_only` in
`capabilities` sagt, dass die Firmware es *kann*; `listen_state` sagt, ob sie
es *tut*.

## Das Sitzungsprotokoll

All das wird ausgehandelt: Der Echo kündigt `oww_local_only` an, der
Controller kündigt `listen_session` in seinem `ack` an. **Lokales Zuhören
greift nur, wenn beides da ist.** Gegen einen älteren Controller behält der
Echo sein bisheriges Verhalten (durchgehender Strom, Auslösen auf das eigene
Wecken hin), weil ein alter Controller auf ein Wecken des Geräts nur dann
handelt, wenn Frames ankommen.

### Nachrichten

Echo → Controller, `/control`:

| `type` | Felder | Bedeutung |
|--------|--------|-----------|
| `listen_state` | `state` (`local`/`stream`/`degraded`), `reason?` | Bei jeder Änderung gesendet und nach jedem `ack` |
| `oww_wake` | `score`, `threshold`, `ageMs`, **`session`**, **`floor`**, **`barge`** | Ein lokales Wecken hat Sitzung `session` eröffnet. `ageMs` ist, wie lange das letzte Frame des Wakewords her **aufgenommen** wurde. `floor` ist der Rauschteppich des Echos (RMS). `barge` ist wahr, wenn der Lautsprecher spielte |
| `listen_end` | `session`, `reason` | Der Echo hat eine Sitzung selbst geschlossen (`ack_timeout`, `max_open`, `muted`, `link`) |

Controller → Echo, `/control`:

| `type` | Felder | Bedeutung |
|--------|--------|-----------|
| `listen_ack` | `session` | Das Wecken wurde angenommen; stoppt die Uhr für `ackTimeout` |
| `listen_close` | `session`, `reason` | Die Sitzung beenden. **Ignoriert, wenn `session` nicht die offene ist** |

`mic_stop` bedeutet beim privaten Zuhören etwas anderes: Es beendet einen
begrenzten Gesprächsstrom, aber nie eine Sitzung (die enden nur über ihre id)
und nie das lokale Zuhören des Echos, das einen Barge-in über die Antwort
hört. `mic_start` mit `lock_mic:true` ersetzt den lokalen Wake-Strom für die
Dauer des Gesprächs.

Echo → Controller, `/data`:

| Code | Aufbau | Bedeutung |
|------|--------|-----------|
| `0x07` | `[0x07][session u32 BE][seq u16 BE][PCM]` | Sitzungston, mono `S16_LE` 16 kHz |

### Warum Sitzungen nummeriert sind

Steuerung und Daten laufen über verschiedene Sockets, der erste Ton einer
Sitzung kann also vor oder nach ihrem `oww_wake` ankommen, und ein spätes
Frame einer Sitzung kann eintreffen, wenn der Controller schon weiter ist.
Jedes Frame mit seiner Sitzung zu kennzeichnen macht beides harmlos: Der
Controller hält Frames für eine Sitzung zurück, von der er noch nichts weiß
(begrenzt, `PENDING_MAX_S`), liefert sie aus, sobald das Wecken ankommt, und
verwirft Frames jeder Sitzung, die er geschlossen hat. Ungekennzeichneter Ton
wurde über ein Flag geleitet, und ein Frame auf der falschen Seite eines
Flag-Wechsels wurde zur ersten halben Sekunde des *nächsten* Befehls.

### Der erste Ton einer Sitzung

Der Echo hält einen Ring des zuletzt verarbeiteten Tons (`ringMs`, 2 s) mit
der Aufnahmezeit jedes 80-ms-Frames. Ein Wecken meldet die Aufnahmezeit des
überschreitenden Frames; die Sitzung beginnt mit jedem Frame aus dem Ring, das
**danach** aufgenommen wurde, und läuft dann live weiter. Das bestehende
`VOICE_PREROLL_DISCARD` des Controllers entfernt das Ende des Wakewords, genau
wie bei einem Wecken, das der Controller erkannt hat.

Die Zeitstempel kommen aus einem einzigen `time.Now()`, das genommen wird, wenn
das Frame sowohl an die Bewertung als auch an den Ring geht — die Wartezeit in
der Warteschlange der Bewertung (bis zu 640 ms unter Last) verschiebt den
Sitzungsbeginn also nie, und `ageMs` misst ab der Aufnahme statt ab dem Ende
der Inferenz.

## Wakeword-Schwellen auf dem Echo

Der Echo bewertet gegen:

- `owwThreshold` im Normalfall;
- `bargeInThreshold`, während sein Lautsprecher eine Antwort oder einen Alarm
  spielt, **oder** Musik, wenn Barge-in aktiviert ist — dieselbe Regel, die
  der Controller anwendet;
- und solange diese niedrigere Schwelle gilt, braucht es **zwei
  aufeinanderfolgende Frames** darüber (die Regel aus `em_barge.decide`), weil
  ein einzelnes Frame an einer Schwelle zehnmal unter der Weckschwelle auf die
  eigene Stimme des Assistenten ansprang.

Ist Barge-in aus, wird ein Wecken, das während einer Antwort gehört wird,
trotzdem gemeldet, und der Controller lehnt es ab (`listen_close`) — die Regel
steht damit an einer Stelle.

## Arbitrierung

Wer zuerst **hört**, gewinnt, nicht wer zuerst ankommt. Jeder Anspruch trägt
die Zeit, zu der sein Ton **aufgenommen** wurde, in der Uhr des Controllers,
und so gemessen, dass Zeit auf der Leitung sie nicht verschieben kann. Eine
Heim-WLAN-Verbindung verliert Pakete, TCP sendet sie erneut, und eine
Nachricht kann Sekunden zu spät eintreffen; die Ankunftszeit ist genau die
Zahl, die lügt.

- **Ein Wecken, das der Controller bewertet hat**, wird auf das
  überschreitende Frame datiert. Der durchgehende Strom sendet jedes
  80-ms-Frame, Stille eingeschlossen, Frame *n* wurde also *n* × 80 ms nach
  Beginn des Stroms aufgenommen; wann das war, lernt der Controller aus den
  Frames, die mit der geringsten Verzögerung ankamen
  (`em_listen.CaptureClock`, ein gleitendes Minimum, das der Drift der Uhr des
  Echos gegen unsere folgt). Ein Frame, das eine Sekunde in einer
  Wiederholung hing, wird trotzdem auf seine Aufnahme datiert, und eines, das
  in der Warteschlange des Controllers lag, bevor es bewertet wurde, ebenso.
- **Ein Wecken, das der Echo erkannt hat**, trägt `capturedMono`, den
  Aufnahmezeitpunkt auf der monotonen Uhr des Echos. Der Controller bildet
  diese Uhr auf seine eigene ab, aus den Ping-Antworten, die er ohnehin alle
  5 s austauscht und die jeweils das `mono` des Echos tragen: Die Antwort mit
  der kürzesten Laufzeit der letzten zwei Minuten legt die Abbildung auf die
  Hälfte dieser Laufzeit genau fest (`em_listen.DeviceClock`, die Regel, die
  auch NTP nutzt). Eine Wiederholung verlängert eine Laufzeit nur, sie wird
  also nie die gewählte. Firmware, die kein `capturedMono` sendet, fällt auf
  Ankunft − `ageMs` − die halbe geglättete RTT zurück, was nur dann stimmt,
  wenn die Nachricht nicht verzögert wurde.

Unkorrigiert bleibt die geringste Verzögerung, die irgendein Frame oder Ping
hatte — ein paar Millisekunden — und die Sendebündelung des Echos, höchstens
ein 80-ms-Frame; beides liegt deutlich innerhalb von `wakeArbitrationMs`.
Keine der beiden Schätzungen liegt je später als die Ankunft oder mehr als
3 s davor.

Ein Anspruch tritt zurück, wenn er innerhalb von `wakeArbitrationMs` des
aktuellen Gewinners gehört wurde, wann immer er eintrifft. Ein gewährter
Anspruch wird **nie widerrufen** — das schnitte ein Gespräch ab, das schon
zuhört —, der Gewinner ist also der erste eintreffende Anspruch unter denen,
die innerhalb des Fensters gehört wurden, und der Gewinner wird
`wakeArbitrationMs` plus 3 s gehalten, damit ein später Anspruch ihn noch
findet. Das Halten kostet nichts: Ein eigenes Wecken in einem anderen Raum
wird danach unterschieden, wann es gehört wurde, nicht wann es ankam. Die 3 s
sind der Ack-Timeout des Echos; ein privates Wecken, das später kommt, hat
seine Sitzung bereits geschlossen, und der Controller ignoriert ein Wecken für
eine Sitzung, die der Echo geschlossen hat.

**Eine gemischte Flotte wartet, eine einheitliche nicht.** Erkennen manche
Echos das Wakeword selbst und werden andere vom Controller bewertet, erreichen
die beiden Wege den Arbiter unterschiedlich schnell — der erste eintreffende
Anspruch ist dann nicht der zuerst gehörte: Am 2026-09-24 traf ein Echo in 10 m
Entfernung, der auf dem Gerät erkannte, 16 ms vor einem ein, der einen Meter
vom Sprecher stand und vom Controller bewertet wurde, und nahm das Gespräch.
Auf einer gemischten Flotte wird der erste Anspruch deshalb bis 250 ms nach
dem Zeitpunkt gehalten, zu dem er gehört wurde (`MIXED_HOLD_S`, abzüglich
dessen, was er schon auf der Leitung verbracht hat), jeder bis dahin innerhalb
des Fensters gehörte Anspruch wird eingesammelt, und der **zuerst** gehörte
gewinnt. Widerrufen wird nichts: Bis das Halten endet, hat niemand das
Gespräch. Eine Flotte, die auf eine Art erkennt, läuft unter gleichen
Bedingungen und gewährt sofort dem ersten Eintreffen, wie oben. Ein Echo,
dessen Modus noch nicht bekannt ist, zählt als anders, die Flotte hält also,
statt zu raten. Ein Barge-in während der Wiedergabe feuert auf dem zweiten
zweier Frames und wird vom ersten datiert.

Die Protokollzeile zum Wecken meldet bei einem vom Controller bewerteten
Wecken, wie lange nach der Ankunft es bewertet wurde und wie lange sein Frame
unterwegs war.

## Was jeder Modus kostet und verliert

| | Auf diesem Echo | Auf dem Controller |
|--|-----------------|--------------------|
| CPU des Echos | ~0,4 eines Kerns, dauerhaft | nichts für das Wakeword |
| WLAN | nichts im Leerlauf | ~32 KB/s pro Echo, dauerhaft |
| Zähler für Beinahe-Treffer im Dashboard | `—`: Die Fensterstatistik des Echos trägt seinen Spitzenwert | auf dem Controller gezählt |
| Vergleichswert des Controllers (`ctrl_wake_score`) | NULL — es gibt nichts zu vergleichen, und es wird nicht als Fehltreffer protokolliert | aufgezeichnet |
| Übersteht einen Neustart des Controllers | Wecken wird weiterhin erkannt, niemand antwortet | nein |

Fehlende Messungen werden als NULL gespeichert, nie als 0: Ein Echo, der
nichts gesendet hat, hat keinen Controller-Wert — das ist nicht dasselbe wie
ein Controller, der null bewertet hat.
