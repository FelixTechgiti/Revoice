# Den Echo Dot Gen 2 (biscuit) rooten

> **Du tust das auf eigenes Risiko. Wir übernehmen keine Verantwortung für
> negative Folgen.**

> **⚠️ Installiere `amonet-biscuit` v2.0.0 NICHT auf einem Echo, auf dem
> Revoice unter FireOS 5 läuft.**
> Version 2.0.0 des Unlocks (10. September 2026) ersetzt die Bootloader des
> Echo, und danach bootet FireOS 5 nicht mehr — ein funktionierendes Gerät
> hört also auf zu funktionieren, und es gibt keinen sicheren Weg zurück. Der
> XDA-Thread fordert entsperrte Nutzer inzwischen zum Update auf. Wenn auf
> deinem Echo Revoice unter FireOS 5 läuft: tu es nicht.
>
> - **Du entsperrst gerade einen neuen Echo?** Nimm **amonet-biscuit v1.1.0**,
>   das weiterhin im XDA-Thread hängt. Auf diesem Weg hat das Projekt mit
>   großem Abstand die meisten Gerätestunden.
> - **Schon auf v2.0.0 aktualisiert?** **Versuche nicht zurückzukommen**, weder
>   durch Flashen von FireOS 5 noch eines älteren amonet. v2.0.0 hat Preloader,
>   LK und TrustZone überschrieben, und die alten von Hand zurückzuschreiben
>   ist genau der Weg, auf dem ein Echo hart gebrickt wird. Dein Echo bleibt
>   auf FireOS 6 — und das ist **keine Sackgasse mehr**: **emOS läuft auf dem
>   Kernel von FireOS 6** (erstmals am 12. September 2026 auf echter Hardware
>   gebootet). Nimm also den **emOS-Weg** des Assistenten, der ein v2-Gerät
>   annimmt. Der FireOS-Weg kann dort nicht funktionieren und verweigert sich,
>   weil er das eigene Android 5 des Geräts startet.
>
>   Zwei ehrliche Einschränkungen dazu, weil es deine Hardware ist und die
>   Sache neu: Es ist bisher auf **einem** Echo gebootet worden, und **noch
>   kein mit v2.0.0 entsperrter Echo ist vollständig durch den Assistenten
>   gelaufen**. Der Schritt „Boot-Image sichern" ist dein Rückweg nach einem
>   missglückten Flash — bewahre diese Datei woanders auf als auf dem Gerät.

Revoice braucht einen Echo Dot Gen 2, der bereits entsperrt ist und FireOS 5
läuft. Zwei getrennte Arbeiten bringen dich dorthin, und sie tragen sehr
unterschiedliche Risiken.

## Hardware

- Amazon Echo Dot 2. Generation (RS03QR, 2016)
- Codename: biscuit
- SoC: MediaTek MT8163, Quad-Core ARM Cortex-A53 @ 1,5 GHz
- RAM: 512 MB
- Betriebssystem: FireOS 5 (Android 5.1, API 22) — siehe den Hinweis unten zu
  FireOS 6
- Micro-USB-Kabel erforderlich

> **Welches FireOS bootet, hängt davon ab, welches amonet du benutzt hast.**
> Bis einschließlich v1.1.0 booten nach dem Entsperren nur ROMs auf
> FireOS-5-Basis; R0rt1z2s Thread sagte, das Flashen von FireOS 6 „may result
> in a (soft) brick". v2.0.0 (10. September 2026) dreht das um: FireOS 6
> bootet, FireOS 5 nicht mehr.
>
> Was sich ändert, sind die Bootloader, nicht die Bitbreite des Kernels. Der
> v2.0.0-Installer schreibt einen neueren Preloader, LK und TrustZone auf das
> Gerät, und der Kernel von FireOS 5 läuft darauf nicht. Sein LK-Patch startet
> einen 32- oder 64-Bit-Kernel je nachdem, was das Boot-Image verlangt — die
> Bitbreite war also nicht das Hindernis. (Das korrigiert eine frühere Fassung
> dieser Notiz, die es auf die Signaturkette der TrustZone schob.)
>
> Revoice läuft, emOS eingeschlossen, auf FireOS 5 und braucht daher für
> **diesen** Weg **v1.1.0**. Für diese Version ist `Fire OS 6.5.7.0
> (NS6570/6077)` weiterhin relevant: ihre Anleitung sagt dir, du sollst vor dem
> Entsperren *darauf* aktualisieren, weil der Exploit die Firmware-Partitionen
> unterwegs herabstuft.
>
> **Die Firmware läuft auf FireOS 5; emOS läuft auf beiden.** Revoices eigene
> Firmware zielt auf die Android-Userspace-Schicht, und die ist auf beiden
> FireOS-Versionen 32-bittig — die Binärdatei ist also dieselbe. Was sich
> unterscheidet, ist emOS' init, das zum Kernel passen muss: 64 Bit unter
> FireOS 5, 32 Bit unter FireOS 6. Der Assistent wählt das, indem er die
> Architektur aus deinem eigenen gesicherten Boot-Image liest, statt dich zu
> fragen.
>

## Was du brauchst

Für das Entsperren selbst (die maßgebliche Liste steht in R0rt1z2s Thread):

- **Linux-Rechner** mit installiertem ADB und fastboot — siehe den Hinweis
  unten zu macOS
- Python 3 (fürs Patchen des Boot-Images und Anlegen der Magisk-Datenbank)
- Die folgenden Dateien heruntergeladen und bereit:
  - `amonet-biscuit-v1.1.0.zip` oder `v2.0.0` — aus R0rt1z2s XDA-Thread.
    v2.0.0 heißt FireOS 6 und damit emOS; siehe den Hinweis ganz oben auf
    dieser Seite. Der Rest dieser Liste beschreibt den FireOS-5-Weg.
  - `update-kindle-csm_biscuit-272.6.8.0_user_680767620.bin` —
    FireOS-5-Firmware (**genau dieser Build** — siehe unten)
  - `f1r30s.zip` — aus R0rt1z2s XDA-Thread. Tut vier Dinge, nicht eines:
    aktiviert ADB und den UART-Konsolenzugang, blockiert Amazons
    OTA-Domänen in `/system/etc/hosts`, damit sich das Gerät nicht selbst
    aktualisieren kann, und deaktiviert dm-verity. **Flashe es immer nach
    einem Stock-Firmware-Image, sonst startet das System nicht** — ein
    Stock-Flash stellt Verity gegen eine Partitionstabelle wieder her, die
    das Entsperren verändert hat. Lass sie danach auf dem Speicher des Echos
    liegen: Der FireOS-Assistent liest sie in Schritt 3 aus
    `/sdcard/f1r30s.zip`. Ist sie nicht da, fragt er danach, prüft sie und
    kopiert sie hinüber.
  - `Magisk-v17.3.zip` — von
    [GitHub](https://github.com/topjohnwu/Magisk/releases/tag/v17.3)
  - `server` — kompiliertes Revoice-Binary (ARM, API 22)

> **Welcher FireOS-5-Build?** R0rt1z2s Thread listet sechs, die auf einem
> entsperrten Dot booten, und Revoice wird gegen genau einen entwickelt und
> getestet: **Fire OS 5.5.5.4**, `272.6.8.0_user_680767620`. Jedes Gerät in
> der Flotte dieses Projekts läuft damit. Die älteren Builds sind nicht als
> kaputt bekannt — sie sind hier schlicht ungetestet, und
> Firmware-Voreinstellungen unterscheiden sich zwischen Builds auf Weisen,
> die bis ins USB- und ADB-Verhalten reichen. Wenn du die Wahl hast, nimm
> diesen. Wenn du bereits ein Gerät auf einem anderen Build hast und sich
> etwas seltsam verhält, ist das die erste Angabe für eine Fehlermeldung.
>
> Prüfe deinen Stand mit `adb shell getprop ro.build.version.name`. Der
> Einrichtungsassistent liest ihn im ersten Schritt und schreibt ihn ins Log.

> **Warum Magisk 17.3?** Neuere Versionen haben die Unterstützung für Android
> 5.1 (API 22) fallen lassen. 25.x installiert sich, aber der Daemon scheitert
> still. 17.3 ist die letzte Version, die auf diesem Gerät zuverlässig
> funktioniert.

> **Nimm für das Entsperren Linux.** Von `brick.sh` wurde berichtet, dass es
> unter macOS scheitert, und die Geräte des Projekts wurden von einer
> Linux-Installation auf einem Mac entsperrt und nicht aus macOS heraus. Ein
> Live-USB-Stick genügt — das Entsperren ist der einzige Schritt, der ihn
> braucht. Das Entsperren ist R0rt1z2s Arbeit, Fragen dazu gehören also in den
> XDA-Thread; das hier ist nur eine Notiz darüber, was beobachtet
> funktioniert.
>
> **ADB-Stabilität unter Linux:** Linux verwaltet die Energie von USB-Geräten
> von Haus aus aggressiv, was ADB-Abbrüche verursacht. Schalte das
> automatische Aussetzen vor dem Start ab:
> `echo -1 | sudo tee /sys/bus/usb/devices/*/power/autosuspend`.

Für die Revoice-Hälfte braucht der Einrichtungsassistent nur einen
**Chromium-basierten Browser** (Chrome oder Edge — er spricht per WebUSB mit
dem Gerät) und einen laufenden Controller. Er holt die Firmware selbst, das
`server`-Binary oben brauchst du also nur, wenn du von Hand einrichtest.

---

## Das Gerät entsperren — R0rt1z2s amonet-biscuit

Das dauerhafte Entsperren, der Bootrom-Exploit und TWRP für dieses Gerät sind
**R0rt1z2s** Arbeit, dokumentiert und gepflegt hier:

- [amonet-biscuit — unlock, root, TWRP, unbrick](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-dot-2nd-gen-2016-biscuit.4761416/)
  im XDA-Forum

Folge diesem Thread, nicht dieser Seite. Wir verlinken ihn, statt ihn zu
kopieren, weil eine Kopie veraltet, ohne dass es jemand merkt. Sollten die
beiden sich je widersprechen, hat der Thread recht — **mit einer Ausnahme:
einem Echo, auf dem Revoice unter FireOS 5 bereits läuft.** Das Update auf
v2.0.0 nimmt ihm FireOS 5, siehe die Warnung ganz oben auf dieser Seite. Für
ein neues Gerät entscheidet die Version, welcher Weg des Assistenten offen
steht: v1.1.0 lässt es auf FireOS 5 und damit beide Wege, v2.0.0 bringt es auf
FireOS 6, wo nur der emOS-Weg geht.

**Das ist der Teil, der ein Gerät ruinieren kann.** Er führt einen
Bootrom-Exploit aus, verändert die Partitionstabelle und löscht `userdata`.
Ein Fehler hier kann einen Dot so weit soft-bricken, dass die Rettung heißt,
das Gehäuse zu öffnen und Kontakte auf der Platine zu brücken. Lies zuerst den
Thread, und fang nicht auf einem Gerät an, dessen Verlust du dir nicht leisten
kannst.

## Wo Revoice übernimmt

Alles Weitere setzt voraus, dass du bereits hast:

- Einen Echo Dot Gen 2 (**biscuit**) mit angewandtem dauerhaftem Entsperren
- **TWRP** installiert und startfähig
- **FireOS 5** installiert (amonet v1.1.0) oder **FireOS 6** in beiden Slots
  (v2.0.0)

Sind die erledigt, übernimmt Revoice. Der Einrichtungsassistent im Dashboard
erledigt den Rest — siehe den [Schnellstart](quickstart.md). Er beginnt bei
einem Gerät, das bereits in diesem Zustand ist; er führt den Exploit nicht
aus.

### Der Assistent hat zwei Abläufe, und sie schreiben Unterschiedliches

**Das ist vor dem Start wichtig, weil sie nicht gleich gut umkehrbar sind.**

- **emOS** — die aktuelle Voreinstellung. Neun Schritte, alle innerhalb von
  TWRP. Er legt deine Boot-Partition in Verwahrung und gibt dir die Datei,
  dann ersetzt er diese Partition durch ein Image aus deinem eigenen Kernel
  und deinen Device Trees plus unserer init. Das Ergebnis betreibt überhaupt
  keine Amazon-Userspace-Software. Siehe
  [`emos/README.md`](../emos/README.md).
- **FireOS** — dreizehn Schritte, auf dem ersten Schritt des Assistenten
  wählbar (oder durch Anhängen von `?flow=fireos` an die Dashboard-URL).
  Behält Android und ergänzt Root: den SELinux-cmdline-Patch, Magisk und die
  Datenbank der Root-Freigaben. Diesen Weg ist jedes Gerät im Feld gegangen.

Beide lassen bei einem gescheiterten Schritt das Gerät in TWRP zurück und
sagen das auch. Der Unterschied, auf den es danach ankommt:

**Ein Gerät auf emOS hat kein adb, der Assistent sieht es also erst wieder,
wenn es in TWRP ist.** Führe `/init recovery` auf der USB-Konsole von emOS aus
(ab emOS 0.4), oder halte die Recovery-Taste beim Anlegen der Stromversorgung
gedrückt — dann nimmt der erste Schritt des Assistenten es an. Aus TWRP gibt
es zwei Wege zurück zu FireOS:

- **Das gesicherte Boot-Image zurückschreiben.** Rund zehn Sekunden, und
  `/data` bleibt unangetastet.
- **Von Hand auf Stock zurück** (amonet v1.1.0): Cache und Daten löschen, das
  FireOS-5-Image per Sideload aufspielen und danach `f1r30s.zip` flashen. Das
  löscht `/data` und nimmt Revoice, seine Konfiguration und seine
  Zugangsdaten mit. **`f1r30s.zip` ist nicht optional** — ein Stock-Flash
  stellt dm-verity gegen eine Partitionstabelle wieder her, die das Entsperren
  verändert hat, und ohne die Datei bootet das Gerät nicht.

Bewahre das verwahrte Boot-Image auf, das dir der emOS-Ablauf in Schritt 3
gibt. Es zurückzuschreiben dauert etwa zehn Sekunden, lässt `/data` in Ruhe
und ist das Rückgängig für alles Weitere.

## Was Revoice schreibt und was nicht

Dieses Gerät hat mehrere Schichten unterhalb des Betriebssystems, und Revoice
schreibt immer nur in die FireOS-Schicht. Die Partitionsnummern und -namen in
diesem Abschnitt wurden auf einem Gerät mit amonet v1.1.0 gemessen. v2.0.0
stellt die ursprüngliche Partitionstabelle wieder her, dort sind `boot_a` und
`boot_b` also die Kernel-Partitionen (`p10`, `p11`), ohne Payload-Partition
und ohne `/dev/block/other-boot`; der Assistent beherrscht beide Layouts. Die
unterste zuerst:

| Schicht | Was es ist | Von Revoice beschrieben |
|---|---|---|
| Preloader | Erste Bootstufe. Zählt Bootversuche je Slot. | Nein |
| LK (Bootloader) | Woher `lk_build_desc` und `unlock_status` kommen. amonet patcht das. | Nein |
| amonets Entsperr-Payload | Lädt den echten Kernel nach. `mmcblk0p17` / `p18`. | Nein |
| TWRP (Recovery) | | Nein, der Assistent führt darin nur Befehle aus |
| FireOS-Kernel und -Ramdisk | `mmcblk0p10` / `p11`. | **Ja**, ein Schreibvorgang |
| `/system`, `/data` | FireOS-Userspace. | Ja, nur Dateien |

Es gibt in beiden Abläufen genau einen Partitionsschreibvorgang, und TWRP
zeigt diese Partition als `/dev/block/other-boot`. Was hineingeht,
unterscheidet sich je Ablauf: Der Schritt „Patch Boot Image" des
**FireOS**-Ablaufs ergänzt am bereits vorhandenen Kernel die
SELinux-permissive-cmdline und den init-Eintrag `service revoice`, während
der **emOS**-Ablauf die Partition durch ein Image ersetzt, das aus genau
diesem Kernel und diesen Device Trees neu gebaut wurde. Keiner von beiden
rührt eine Schicht über `Nein` in der Tabelle an.

**Das By-Name-Verzeichnis bedeutet in TWRP etwas anderes als in Android**, und
das ist gut zu wissen, bevor man irgendetwas davon für bare Münze nimmt. Auf
Hardware gemessen:

| By-Name-Eintrag | In TWRP | In Android |
|---|---|---|
| `boot_a` | `p10`, der Kernel | `p17`, die Payload |
| `boot_a_x` | `p10`, der Kernel | `p10`, der Kernel |
| `boot_a_amonet` | `p17`, die Payload | nicht vorhanden |

TWRP bildet die bloßen Namen auf die Kernel-Partitionen ab und legt die
Payload ausdrücklich als `*_amonet` offen. Derselbe Name bedeutet also
Gegensätzliches, je nachdem, wo man steht, und `p10` hört auf zwei Namen
zugleich.

Vor dem Schreiben löst der Assistent `/dev/block/other-boot` auf, sammelt
jeden By-Name-Alias dessen, worauf es zeigt, und verweigert, wenn einer davon
eine amonet-Payload-Partition ist. Einen Kernel dorthin zu schreiben würde das
Entsperren zerstören und bedeuten, amonet erneut laufen zu lassen — deshalb
wird es geprüft und nicht angenommen. Er prüft außerdem, dass das gelesene
Image wirklich ein Boot-Image ist, bevor er es patcht, und liest die cmdline
danach von der Partition zurück, statt dem Schreibvorgang zu vertrauen.

Scheitert eine dieser Prüfungen, hält der Assistent an, mit dem Gerät noch in
TWRP — ein Ort, von dem aus man sich erholen kann.

## Wenn ein Gerät nicht bootet

Alles auf oder unterhalb des Bootloaders ist das Gebiet des Entsperrens, und
[R0rt1z2s Thread](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-dot-2nd-gen-2016-biscuit.4761416/)
ist dafür maßgeblich. Eine Sache aus diesem Thread ist es wert, hier wiederholt
zu werden, weil sie zeitkritisch ist:

> **Hör auf, es booten zu wollen.** Der Preloader zählt Bootversuche je Slot,
> und wenn beiden Slots die Versuche ausgehen, bootet das Gerät gar nicht
> mehr.

Aus diesem Zustand kommt man wieder heraus, aber die leichten Wege sind weg:
Der Rückweg heißt, das Gehäuse zu öffnen und einen Pin auf der Platine zu
brücken, um an das Bootrom zu kommen — R0rt1z2s Thread dokumentiert das und
beschreibt es als nicht besonders schwierig. Ein Gerät, das am Kabel in
fastboot oder TWRP sitzt, braucht nichts davon. Eines, das nicht bootet,
wiederholt vom Strom zu trennen, ist es, was aus der ersten Lage die zweite
macht.

## Wie gut ist das getestet?

**Die beiden Abläufe haben sehr unterschiedlich viel Beleg hinter sich, und
die Voreinstellung ist der neuere.**

Acht Geräte haben die Schritte des **FireOS**-Ablaufs ohne Fehler durchlaufen.
Das ist eine kleine Stichprobe, alles am selben Modell von derselben Person —
nimm es also als ermutigend, nicht als abschließend.

Der **emOS**-Ablauf ist von Anfang bis Ende auf Hardware durchgelaufen, aber
erst seit Kurzem und auf einer Handvoll Geräte. Sein erster vollständiger Lauf
gegen ein auf echten Auslieferungszustand zurückgesetztes Gerät scheiterte an
vier verschiedenen Schritten, bevor er funktionierte — alle vier waren Fehler
in den Prüfungen des Assistenten selbst und nicht in den Schreibvorgängen, und
alle vier sind behoben, aber das ist die Reife, die man einpreisen sollte.
Wenn du heute den besser belegten Weg willst, wähle im ersten Schritt des
Assistenten FireOS.

## Wiederherstellung

**Die Regel, auf die es ankommt: Wenn ein Gerät nicht bootet, trenne es nicht
immer wieder vom Strom.** Eines, das nicht hochkommt, wiederholt vom Strom zu
trennen, macht aus einem per Kabel rettbaren Gerät eines, bei dem das Gehäuse
geöffnet und ein Pin gebrückt werden muss. Geh stattdessen nach TWRP — es ist
eine Tastenkombination entfernt und kostet etwa zehn Sekunden, das alte
Boot-Image zurückzulegen.

**TWRP auf einem Gerät erreichen, das nicht bootet:**

1. Strom trennen.
2. Die **Mute**-Taste oder **+** (lauter) gedrückt halten und weiter halten.
   Welche der beiden es ist, hängt davon ab, mit welcher amonet-Version das
   Gerät entsperrt wurde; [R0rt1z2s
   Thread](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-dot-2nd-gen-2016-biscuit.4761416/) nennt sie.
3. Strom anlegen, die Taste weiter gedrückt.
4. Warten, bis der Ring wechselt — das ist die Bestätigung, dass du im
   Recovery bist, und dann kannst du loslassen.

`adb reboot recovery` ist der einfache Weg und braucht ein Gerät, das schon
oben ist — genau das, was du hier nicht hast.

### Wenn ein Schritt des Assistenten gescheitert ist

Das Gerät ist noch in TWRP, und der Assistent sagt das. Verbinde neu und nutze
**Restore escrowed boot image** — es schreibt das Image zurück, das im
Verwahrungsschritt von deinem eigenen Gerät gelesen wurde, prüft es gegen die
Partition und lässt `/data` unangetastet. Wenn du die Seite seitdem neu
geladen hast, wähle die Datei `revoice-stock-boot-*.img`, die du in jenem
Schritt heruntergeladen hast; es sind dieselben Bytes.

### Wenn der erste Start nach dem Flashen von emOS nicht hochkommt

Der Lichtring sagt dir, in welchem Fall du bist:

| Ring | Was es bedeutet | Was zu tun ist |
|---|---|---|
| Füllt sich, dann weiß, dann verblassend | Läuft und ist im Netz | Nichts — fertig, etwa 30 Sekunden |
| Zwei leuchtende Segmente unten, pulsierend | Wartet auf das Netzwerk | Nichts — das ist der größte Teil des Starts |
| Dauerhaft bernsteinfarben | emOS stellt sein eigenes letztes funktionierendes Image wieder her | **Lass es.** Es startet sich selbst neu |
| Rot, stehend | Eine Bootstufe ist gescheitert | Rettbar — nach TWRP gehen und wiederherstellen |
| Ein Segment kreist um einen vollen blauen Ring, länger als eine Minute | emOS ist nie gestartet | Nach TWRP gehen und wiederherstellen |

Spiel das Image aus TWRP zurück, wenn der Ring rot und stehend endet — oder
wenn ein einzelnes Segment länger als eine Minute um einen vollen blauen Ring
kreist. Alles andere heißt, dass emOS startet; dann lass es zu Ende laufen
(#642). Das Kreisen heißt, dass der Kernel hochkam und unsere init nie lief —
nichts auf dem Gerät wird sich also selbst richten. Es ist die eigene
Boot-Animation des Kernels, die noch läuft, weil der Userspace den Ring nie
beansprucht hat.

Die bernsteinfarbene Zeile ist gerade deshalb wichtig, damit du *nicht*
eingreifst: emOS zählt Starts, die nie das Netzwerk erreicht haben, und legt
nach dreien sein eigenes funktionierendes Image zurück und startet neu. Das zu
unterbrechen ist die eine Art, es schlimmer zu machen.

### Wenn es auch TWRP nicht erreicht

Das ist das Gebiet des Entsperrens und nicht unseres, und R0rt1z2s Thread
behandelt Wiederherstellung und Unbricking.

## Dank

- **R0rt1z2** — [amonet-biscuit](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-dot-2nd-gen-2016-biscuit.4761416/):
  dauerhaftes Entsperren, TWRP und Unbrick für dieses Gerät
- **Dragon863** — [EchoCLI](https://github.com/Dragon863/EchoCLI):
  Forschung zum getetherten Root
- **Binozo** — [GoTinyAlsa](https://github.com/Binozo/GoTinyAlsa) und das
  ursprüngliche EchoGo-SDK

---

# Referenz zur Handarbeit

Der Assistent führt die folgenden Schritte für dich aus. Es sind die des
**FireOS-Wegs auf einem Gerät mit amonet v1.1.0**; der emOS-Weg macht keinen
davon. Sie stehen hier für alle, die von Hand einrichten, einen
Assistentenschritt debuggen oder genau wissen wollen, was mit ihrem Gerät
passiert, bevor sie etwas automatisch machen lassen.

## Schritt 3 — Das Boot-Image auf SELinux permissive patchen

Das ist der Schritt, der nirgendwo sonst dokumentiert ist.

Der Little-Kernel-Bootloader (LK) verdrahtet `androidboot.selinux=enforce`
fest in die Kernel-Kommandozeile — das wird gesetzt, bevor Android überhaupt
lädt, und es ist das, was jeden Versuch blockiert, SELinux zur Laufzeit
abzuschalten. Du kannst als Shell weder `setenforce 0` noch `resetprop` noch
`magiskpolicy` benutzen. Der Kernel lässt es nicht zu.

Die Lösung: Wir hängen `androidboot.selinux=permissive` an das cmdline-Feld
des Boot-Images selbst an. LK fügt dieses Feld mitten in seine eigenen
Parameter ein und ergänzt danach sein `enforce`, es landen also beide Werte
auf der Kernel-Kommandozeile, unserer zuerst — und **der erste gewinnt**. Aus
`androidboot.*` wird über Androids init eine `ro.boot.*`-Eigenschaft,
schreibgeschützte Eigenschaften lassen sich nur einmal schreiben, und der
zweite Satz wird abgewiesen. Am 2026-09-06 auf zwei Geräten gemessen:
`getenforce` Permissive, `ro.boot.selinux` permissive.

Denk darüber nicht wie über einen Kernel-Parameter nach, wo ein späterer Wert
einen früheren überschriebe. `androidboot.selinux` ist keiner — die eigenen
Schalter des Kernels heißen `selinux=` und `enforcing=`, und die setzt hier
nichts.

> **Hinweis:** Die cmdline ist eine nullterminierte ASCII-Zeichenkette in
> einem 512-Byte-Feld an fester Position (Byte 64) im Header des
> Android-Boot-Images. Wir patchen sie direkt, statt magiskboot zu benutzen,
> das in dieser Version keine cmdline-Änderung unterstützt.

### Aus TWRP heraus magiskboot entpacken und das Boot-Image holen:

```bash
adb shell 'mkdir -p /tmp/work /tmp/bin'
adb shell 'unzip /sdcard/f1r30s.zip bin/magiskboot -d /tmp/'
adb shell 'chmod 755 /tmp/bin/magiskboot'
adb shell 'dd if=/dev/block/other-boot of=/tmp/work/boot.img bs=1048576'
adb pull /tmp/work/boot.img boot_fresh.img
```

### Die cmdline auf deinem Rechner patchen:

Das **hängt an**, was FireOS dort schon hingeschrieben hat. Eine frühere
Fassung dieser Anleitung nullte das ganze 512-Byte-Feld und schrieb einen
kurzen Ersatz hinein, was FireOS' eigene Argumente stillschweigend verwarf —
`rootwait`, `ro`, `init=/init`, `buildvariant`, die Abstimmung des
`lowmemorykiller` und `veritykeyid`. Geräte booteten trotzdem, weil LK `root=`
und `androidboot.hardware` liefert und Kernel-Voreinstellungen den Rest
abdeckten, es fiel also lange nicht auf. Trotzdem ist es das Falsche, was man
mit dem Boot-Image von jemandem tun kann.

```python
python3 - <<'EOF'
ARG = b'androidboot.selinux=permissive'
START, END = 64, 576          # das 512-Byte-cmdline-Feld

with open('boot_fresh.img', 'rb') as f:
    data = bytearray(f.read())

if bytes(data[:8]) != b'ANDROID!':
    raise SystemExit("Not an Android boot image — refusing to patch.")

field = data[START:END]
used  = field.index(0) if 0 in field else len(field)
existing = bytes(field[:used])
print("Old cmdline:", existing.decode(errors='replace'))

if ARG in existing.split():
    print("Already patched — nothing to do.")
elif any(a.startswith(b'androidboot.selinux=') for a in existing.split()):
    raise SystemExit(
        "This image already sets androidboot.selinux to something else. "
        "Appending would lose to it, because the FIRST value wins. Fix that "
        "value rather than adding a second one.")
else:
    addition = (b' ' if used else b'') + ARG
    if used + len(addition) >= len(field):      # Platz für den Terminator lassen
        raise SystemExit("Cmdline too long to append without truncating it.")
    data[START + used:START + used + len(addition)] = addition
    data[START + used + len(addition)] = 0
    print("New cmdline:", bytes(data[START:START + used + len(addition)]).decode())

    with open('boot_patched.img', 'wb') as f:
        f.write(data)
    print("Written to boot_patched.img")
EOF
```

Prüfe, dass die neue cmdline deine ursprüngliche mit
`androidboot.selinux=permissive` am Ende ist und vorne nichts fehlt.

### Das gepatchte Image flashen:

```bash
adb push boot_patched.img /tmp/work/boot_patched.img
adb shell 'dd if=/tmp/work/boot_patched.img of=/dev/block/other-boot bs=1048576'
adb reboot
```

### Prüfen:

```bash
adb shell getenforce
# Erwartet: Permissive
```

Prüfe die Kernel-cmdline im logcat, um zu bestätigen, dass beide Werte da
sind:

```
androidboot.selinux=permissive androidboot.selinux=enforce
```

Beide erscheinen — LK hängt seinen Wert immer hinter unseren —, aber das Gerät
landet im permissiven Modus.

---

## Schritt 4 — Magisk 17.3 installieren

Mit permissivem SELinux kann Magisks Daemon jetzt starten und ordentlich
laufen.

```bash
adb reboot recovery
adb push Magisk-v17.3.zip /sdcard/
adb shell twrp install /sdcard/Magisk-v17.3.zip
adb reboot
```

Probiere **noch nicht** `adb shell su -c id` — es hängt. Die Freigabe verlangt
einen Bildschirm zum Bestätigen, und der Echo Dot hat keinen.

---

## Schritt 5 — Die Magisk-Freigabedatenbank vorbelegen

Magisks `su` hängt auf einem bildschirmlosen Gerät, weil es darauf wartet,
dass jemand in einem Dialog auf „Grant" tippt, der nie erscheint. Die Lösung
ist, die Richtliniendatenbank selbst anzulegen und sie vor dem Start
aufzuspielen.

### Auf deinem Rechner:

```python
python3 - <<'EOF'
import sqlite3
conn = sqlite3.connect('magisk.db')
c = conn.cursor()
c.execute('''CREATE TABLE IF NOT EXISTS policies
             (uid INTEGER, package_name TEXT, policy INTEGER,
              until INTEGER, logging INTEGER, notification INTEGER)''')
# uid 2000 = shell, policy 2 = immer gewähren
c.execute("INSERT INTO policies VALUES (2000, 'com.android.shell', 2, 0, 1, 0)")
c.execute("INSERT INTO policies VALUES (0, 'root', 2, 0, 1, 0)")
conn.commit()
conn.close()
print("Done — magisk.db created")
EOF
```

### Aus TWRP heraus aufspielen:

```bash
adb reboot recovery
adb push magisk.db /data/adb/magisk.db
adb shell chmod 600 /data/adb/magisk.db
adb reboot
```

### Root prüfen:

```bash
adb shell su -c id
# Erwartet: uid=0(root) gid=0(root) context=u:r:magisk:s0
```

Wenn du `uid=0(root)` siehst, hast du dauerhaften Root. Starte noch einmal neu
und bestätige, dass er das übersteht.

---

## Schritt 6 — Den Alexa-Stack abschalten

Mit Root funktioniert `pm disable` jetzt. Führe diese einzeln aus:

```bash
# Kern der Alexa-Sprachpipeline
adb shell su -c 'pm disable amazon.speech.davs.davcservice'
adb shell su -c 'pm disable amazon.speech.sim'
adb shell su -c 'pm disable com.amazon.alexa.beaconbroadcaster'
adb shell su -c 'pm disable com.amazon.alexa.externalmediaplayer.fireos'
adb shell su -c 'pm disable com.amazon.wha.mediabrowserservice'

# Whisperjoin (Alexa-Geräteeinrichtung / Cloud)
adb shell su -c 'pm disable com.amazon.whisperjoin.middleware'
adb shell su -c 'pm disable com.amazon.whisperjoin.wss.wifiprovisioner'

# Smart Home und Medien-Agent (Absturzschleife nach dem Abschalten oben)
adb shell su -c 'pm disable com.amazon.device.smarthome.dshs.services'
adb shell su -c 'pm disable com.amazon.mediaplayeragent'

# WLAN-Verwaltung — nur nötig, wenn du das WLAN weg von dem Netzwerk
# umstellen willst, mit dem die Alexa-Einrichtung sich ursprünglich verbunden
# hat. Beide bekämpfen aktiv manuelle Änderungen an wpa_supplicant.conf, indem
# sie ihr eigenes gespeichertes Netzwerkprofil erneut durchsetzen. Die
# vollständige Untersuchung steht im Changelog zu v2.5.0.
adb shell su -c 'pm disable com.amazon.android.service.wifiprofilemanager'
adb shell su -c 'pm disable com.amazon.device.smarthome.adapters.wifi'
# Das obige pm disable stoppt das native Binary SmartHomeWifid NICHT — es wird
# von init über eine Property-Trigger-Kette gestartet, nicht als normale
# Paketkomponente. Das hier verhindert dauerhaft, dass dieser Trigger je
# feuert:
adb shell su -c 'setprop persist.wifi.migrate.complete 0'
```

Starte neu und sieh ins logcat. Du solltest Meldungen „Unable to start
service" für diese Pakete sehen — das ist erwartet und harmlos. Keine
Absturzschleifen.

> **Lass `com.amazon.device.echoaudioservice` aktiviert.** Dieser Dienst
> initialisiert beim Start den Audio-DSP von MediaTek. Ohne ihn startet der
> I2S-Takt nie, und die Audiowiedergabe hängt still. Du kannst Alexas
> Sprachstack abschalten, ohne diesen Dienst anzufassen.
>
> **Was echoaudioservice tatsächlich tut:** Das APK ist ein Rumpf (nur
> Manifest, keine Java-Klassen). Es bringt `audio.primary.mt8163.so` (den
> Audio-HAL des MT8163) dazu, den DSP zu initialisieren, wenn Android den
> Dienst startet. Der HAL macht die eigentliche Arbeit — echoaudioservice ist
> nur der Auslöser.

---

## Schritt 7 — WiFi Direct (p2p0) abschalten

Das Gerät hat eine WiFi-Direct-Schnittstelle (`p2p0`), die die Auswahl der
Multicast-Schnittstelle für mDNS stört. Sie muss heruntergefahren werden,
bevor Revoice startet.

Das erledigt `start_server.sh` — wenn du der vollständigen Anleitung folgst,
ist nichts von Hand zu tun. Zum manuellen Testen:

```bash
adb shell su -c 'ip link set p2p0 down'
```

---

---
## Schritt 8 — Revoice installieren

Revoice läuft als Go-Binary auf dem Gerät. Es abstrahiert die Hardware
(Mikrofon, Lautsprecher, LEDs, Tasten) und verbindet sich über zwei
dauerhafte WebSocket-Verbindungen nach außen zum Revoice-Controller (plus
eine bei Bedarf geöffnete Shell-Ebene). Auf dem Gerät läuft kein HTTP-Server
— keine eingehenden Ports, keine iptables-Regeln nötig.

### Das Binärverzeichnis einrichten (A/B-Slots):

Revoice ab v2.4.4 nutzt A/B-Slots: `server_a` und `server_b` mit
`/data/local/bin/server` als symbolischem Link. Das erlaubt sofortiges
Zurückrollen ohne Übertragung eines Binarys.

```bash
adb shell "su -c 'mkdir -p /data/local/bin'"
adb push server /sdcard/server
adb shell "su -c 'cp /sdcard/server /data/local/bin/server_a && chmod 755 /data/local/bin/server_a && ln -sf server_a /data/local/bin/server && chown root:root /data/local/bin/server_a'"
```

`server_b` beginnt leer. Das erste OTA-Update aus dem Dashboard füllt es.

### Das Startskript anlegen:

Das maßgebliche Skript ist **`controller/device_payloads/start_server.sh`** im
Repository (`device/scripts/start_server.sh` ist ein symbolischer Link
darauf) — der Controller liefert genau diese Datei unter
`/api/provision/start_script` aus (das installiert der
Einrichtungsassistent), je Anfrage frisch von der Platte gelesen. Pflege
keine eigene Kopie; frühere Fassungen dieses Dokuments und von `em_api.py`
hatten eingebettete Kopien, und die sind auseinandergelaufen.

```bash
# Aus dem Wurzelverzeichnis des Repositorys:
adb push device/scripts/start_server.sh /sdcard/start_server.sh
adb shell "su -c 'cp /sdcard/start_server.sh /data/local/bin/start_server.sh && chmod 755 /data/local/bin/start_server.sh && chown root:root /data/local/bin/start_server.sh'"
```

> Das Skript wartet vor dem Start auf `echoaudio` — damit ist der Audio-DSP
> initialisiert. `p2p0` wird heruntergefahren, um mDNS-Störungen zu
> verhindern. Der WLAN-Wake-Lock hindert FireOS daran, die
> Funkschnittstelle schlafen zu legen. Die gesamte Serverausgabe geht nach
> `/tmp/server.log` und ist per
> `adb shell su -c 'cat /tmp/server.log'` zu lesen.

> **Log-Obergrenze (v2.7.1):** `/tmp` liegt im RAM, und das Skript hängt nur
> an — eine Hintergrundschleife im Skript prüft alle 5 Minuten und behält
> jenseits von 5 MB die neuesten 512 KB in `/tmp/server.log.1` und kürzt
> `server.log` an Ort und Stelle (der `O_APPEND`-Deskriptor des Servers macht
> am neuen Dateiende weiter). Der gesamte Log-Fußabdruck bleibt bei ~5,5 MB
> begrenzt. Vorher wurde im Feld ein 45-MB-Log beobachtet.

> Das Skript führt den Server als Unterprozess aus (nicht per `exec`), damit
> SIGTERM per `trap` von Androids init weitergereicht werden kann. Beendet
> sich das Binary dreimal hintereinander in weniger als 15 Sekunden, wird der
> inaktive A/B-Slot per symbolischem Link wiederhergestellt und das Skript
> endet sauber — init startet es mit dem alten Binary neu. Läuft das Binary
> ≥15 s, bevor es abstürzt, wird der Versuchszähler zurückgesetzt (ein
> Betriebsabsturz, kein Auslieferungsfehler).

### Revoice und den Mixer-Dienst in die Ramdisk eintragen:

Die init-Skripte von FireOS 5 liegen in der Ramdisk des Boot-Images. Wir
müssen sie entpacken, `init.csm.project.rc` bearbeiten und wieder einpacken.

Nach TWRP starten:

```bash
adb reboot recovery
```

magiskboot entpacken und das Boot-Image auspacken:

```bash
adb shell 'mkdir -p /tmp/work /tmp/bin'
adb shell 'unzip /sdcard/f1r30s.zip bin/magiskboot -d /tmp/'
adb shell 'chmod 755 /tmp/bin/magiskboot'
adb shell 'dd if=/dev/block/other-boot of=/tmp/work/boot.img bs=1048576'
adb shell 'cd /tmp/work && /tmp/bin/magiskboot unpack boot.img'
adb shell 'mkdir -p /tmp/ramdisk && cd /tmp/ramdisk && cpio -idv < /tmp/work/ramdisk.cpio 2>/dev/null | tail -3'
```

Das init-Skript holen und auf deinem Rechner bearbeiten:

```bash
adb pull /tmp/ramdisk/init.csm.project.rc init.csm.project.rc
```

Hänge die folgenden zwei Dienstblöcke ans Ende von `init.csm.project.rc`. Der
`mixer`-Rumpf muss zuerst kommen — Revoices `Init()` für den Lautsprecher
ruft als ersten Schritt `stop mixer` auf:

```
service mixer /system/bin/sh
    oneshot
    disabled
    user root

service revoice /data/local/bin/start_server.sh
    user root
    group root system
    class late_start
```

Zurückschieben, Rechte richten, wieder einpacken und flashen:

```bash
adb push init.csm.project.rc /tmp/ramdisk/init.csm.project.rc
adb shell 'chmod 750 /tmp/ramdisk/init.csm.project.rc'
adb shell 'cd /tmp/ramdisk && find . | cpio -o -H newc > /tmp/work/ramdisk.cpio'
adb shell 'cd /tmp/work && /tmp/bin/magiskboot repack boot.img'
adb shell 'dd if=/tmp/work/new-boot.img of=/dev/block/other-boot bs=1048576'
adb reboot
```

### Prüfen:

Nach vollständigem Start (rechne mit ~90 Sekunden):

```bash
adb shell "su -c 'getprop init.svc.revoice'"
# Erwartet: running

adb shell "su -c 'cat /tmp/server.log'"
# Erwartet: Initializing... Ready... mDNS browsing...
```

---

---

## Endzustand

Nach dem Assistenten ist ein Echo in einem von zwei Zuständen:

- **emOS:** Die Boot-Partition trägt emOS (unser init und unser Userspace um
  den eigenen Kernel des Echos herum), Revoice startet beim Booten, und
  Android läuft nie. Das ursprüngliche Boot-Image sichert der Assistent, und
  es lässt sich aus TWRP in etwa zehn Sekunden zurückschreiben.
- **FireOS mit Root:** FireOS 5 bootet mit permissivem SELinux und
  Magisk-Root, der Alexa-Stack ist abgeschaltet, und Revoice läuft als
  init-Dienst.

In beiden Fällen bleiben das Entsperren und TWRP an Ort und Stelle, und das
Echo wählt sich von sich aus zum Controller.

Die Liste Punkt für Punkt, die diese Seite früher beschloss, beschrieb die
Firmware im Stand von v2.7 und war veraltet. Was die einzelnen Teile heute
tun, steht in [configuration.md](configuration.md) und
[voice-pipeline.md](voice-pipeline.md), und wie es dahin kam, im
[Journal](../JOURNAL.md).
