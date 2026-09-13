// Revoice UI strings, and the machinery that picks between them.
//
// A plain classic script rather than part of dashboard.jsx, for two
// reasons. The bundle is already 9,700 lines and this would be the largest
// thing in it without being code. And the LANDING page needs the same
// strings and loads no bundle at all — it is served before dashboard.js
// exists, which is the whole point of it being self-contained.
//
// WHICH LANGUAGE EACH THING IS IN is settled by the repo's CLAUDE.md §6 and
// is not uniform: this file is the UI, so it is both. Issues and PR text
// are English; the changelog a user reads before updating is German.
//
// ── The rules the layout has to keep ──────────────────────────────────
//
// German runs about 30% longer than English, and the failures that causes
// are structural rather than cosmetic:
//
//   - no fixed width on a label, and no `white-space: nowrap` on one. A
//     number column may have both, because numbers do not translate.
//   - `min-width: 0` on every text container inside a flex or grid item,
//     or the item refuses to shrink below its content and pushes the row
//     past the edge of its panel.
//   - `text-wrap: pretty` on anything that is a sentence.
//   - units (dBm, ms, %, dB) are not translated, and numbers and dates go
//     through Intl rather than being formatted by hand.
//
// ── Composed sentences are per language, not per template ─────────────
//
// The fleet page's headline is assembled from counts. "One device is
// listening, one is playing music" and "Ein Gerät hört zu, eines spielt
// Musik" do not share a shape — the verb moves, the second clause drops
// its noun differently, and the number words decline. So the composition
// is a FUNCTION per language (see `sentence` below), never one template
// with holes. A template would be right for one language and subtly wrong
// for the other, which is worse than obviously wrong.

(function (global) {
  'use strict';

  // Number words up to nine, because "3 Geräte sind bereit" in a sentence
  // reads as a readout and the readouts are elsewhere. Above nine the digit
  // wins — reading "zwölf" at a glance costs more than it saves.
  const WORDS = {
    en: ['no', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine'],
    de: ['kein', 'ein', 'zwei', 'drei', 'vier', 'fünf', 'sechs', 'sieben', 'acht', 'neun'],
  };

  function cap(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

  const STRINGS = {
    en: {
      // ── Header ──
      settings: 'Settings',
      signOut: 'Sign out',
      controllerVersion: 'Controller',
      reload: 'reload',
      themeToLight: 'Switch to light theme',
      themeToDark: 'Switch to dark theme',
      densityToDense: 'Switch to the dense layout',
      densityToRoomy: 'Switch to the roomy layout',
      language: 'Language',

      // ── Fleet page ──
      online: 'Online',
      inConversation: 'In conversation',
      playing: 'Playing',
      waiting: 'Waiting',
      devices: 'Devices',
      waitingForYou: 'Waiting for you',
      nameAndApprove: 'Name & approve',
      setUpADot: 'Set up an Echo Dot',
      noDevicesYet: 'No devices yet — power on a Revoice device to see it appear here',
      nothingNeedsAttention: 'Nothing needs attention',
      addressUnknown: 'address unknown',
      firstSeen: 'first seen',
      lastKnownAddress: 'Last known address',

      // ── Columns ──
      device: 'Device',
      state: 'State',
      volume: 'Volume',
      latency: 'Latency',
      firmware: 'Firmware',
      roundTrip: 'Control-plane round trip',
      updateAvailable: 'Update available',

      // ── Device states ──
      statePending:   'Pending',
      stateOffline:   'Offline',
      stateMuted:     'Muted',
      stateSpeaking:  'Speaking',
      stateThinking:  'Thinking',
      stateListening: 'Listening',
      statePlaying:   'Playing',
      stateIdle:      'Ready',

      // ── Sidebar ──
      nowPlaying: 'Now playing',
      firmwareSection: 'Firmware',
      checkForUpdates: 'Check for updates',
      checking: 'Checking…',
      deployToAll: 'Deploy to all',
      everyDeviceLatest: 'Every device is on the latest release.',
      noReleaseInfo: 'No release information — the controller could not reach GitHub.',

      // ── Playback ──
      playingNow: 'Playing now',
      source: 'Source',
      level: 'Level',
      ducking: 'Ducking',
      duckingPauses: 'pauses',
      playingNoTrackRow: 'This device is playing; it does not report what.',
      playingNoTrackPanel:
        'This device is playing. Its firmware does not report the track, so '
        + 'there is nothing to name here yet.',

      // ── Approval ──
      unnamedDevice: 'Unnamed device',
      actionRequired: 'Action required',
      approvePrompt: 'Name this device below, then approve it to add it to your fleet.',
      approveHeading: 'What approving does, in the order it happens:',
      approveConfig: 'It receives the fleet configuration.',
      approveSatellite: 'Home Assistant gets a voice satellite and a media player for it.',
      approveEndpoints: 'Its Spotify, AirPlay and Sendspin endpoints become reachable.',
      approveMic: 'Its microphone streams to the controller once it hears the wake word.',

      // ── Landing page ──
      checkingController: 'Checking the controller…',
      readySignIn: 'Ready — sign in',
      firstRunSetup: 'First-run setup',
      apiUnreachable: 'API unreachable',
      signingIn: 'Signing in…',
      creatingAccount: 'Creating account…',
      accountCreated: 'Account created',
      signedInViaHa: 'Signed in via Home Assistant',
      setupToken: 'Setup token',
      username: 'Username',
      password: 'Password',
      createAdmin: 'Create admin account',
      signIn: 'Sign in',
      tokenInLogs: 'The setup token is in the controller logs:',
      haDidNotIdentify:
        'Home Assistant did not identify you to this add-on. Reload the panel, '
        + 'or use the setup token from the add-on log.',

      // -- Configuration (DeviceConfigForm) --
      cfgPlayback: 'Playback',
      cfgPlaybackDesc: 'Response audio: Home Assistant TTS → parametric EQ → resample → device speaker. Presets set the faders; drag any fader for a custom curve.',
      cfgSpeechBoost: 'Speech boost',
      cfgSpeechBoostSub: 'presence boost for voice',
      cfgDuckDepth: 'Duck depth',
      cfgAudioHoldoff: 'Audio hold-off',
      cfgSpeakerProtection: 'Speaker protection',
      cfgSpeakerProtectionSub: 'keeps bass the driver can\'t deliver from muddying the midrange — leave on',
      cfgWakeWord: 'Wake word',
      cfgWakeWordDesc: 'openwakeword scores the continuous mic stream on the controller. Sensitivity sets the detection threshold — attempts that score close but miss are counted as near-misses (Status tab).',
      cfgDeleteModel: 'Delete model',
      cfgSpeexDenoise: 'Speex denoise',
      cfgSpeexDenoiseSub: 'cleans audio before scoring — try in noisy rooms',
      cfgBargeIn: 'Barge-in',
      cfgBargeInSub: 'wake word interrupts playback — enable AEC first',
      cfgBargeThreshold: 'Barge threshold',
      cfgBargeThresholdSub: 'wake confidence needed during playback — raise it if a response cuts itself short',
      cfgArbitrationWindow: 'Arbitration window',
      cfgArbitrationWindowSub: 'ms that the first Echo to hear you silences the others — no added delay; 0 disables',
      cfgWakeDetection: 'Wake word detection',
      cfgMicrophones: 'Microphones',
      cfgMicrophonesDesc: 'Capture from the 7-mic array. Presets steer which perimeter mic is used during voice turns — wake-word listening always uses the centre mic. Gain here is the only gain in the wake path: it sets the level everything downstream hears.',
      cfgMicpga: 'MICPGA',
      cfgMicpgaSub: 'analog gain, before the ADC',
      cfgDigitalGain: 'Digital gain',
      cfgDigitalGainSub: 'ADC digital gain — affects wake + turns',
      cfgMicGain: 'Mic gain',
      cfgMicGainSub: 'fixed gain on the 24-bit capture, pre-16-bit stream',
      cfgBeamAngle: 'Beam angle',
      cfgBeamAngleSub: '-1 = auto (onset-ratio selection)',
      cfgBeamforming: 'Beamforming',
      cfgBeamformingSub: 'perimeter mic lock during turns',
      cfgAec: 'Echo cancel (AEC)',
      cfgAecSub: 'subtracts the device\'s own playback — wake + turns',
      cfgNoiseSuppression: 'Noise suppression',
      cfgNoiseSuppressionSub: 'DTLN denoise on speech-to-text audio only — helps fans/hum, not TV speech',
      cfgAecDelay: 'AEC delay',
      cfgAecTail: 'AEC tail',
      cfgAecTailSub: 'filter length — residual delay error + room reverb',
      cfgEchoReference: 'Echo reference',
      cfgSaveUtterances: 'Save utterances',
      cfgSaveUtterancesSub: 'keeps the last 10 turns\' mic audio on the server — play or download from Activity',
      cfgRing: 'Ring',
      cfgRingDesc: 'Colours for the LED ring during conversations — the solid listening ring and the thinking spinner. The red mute ring and cyan volume arc never change; red always means the mics are off.',
      cfgDecay: 'Decay',
      cfgDecaySub: 'how fast it falls — higher tracks individual syllables',
      cfgAttack: 'Attack',
      cfgAttackSub: 'how fast it rises on a peak',
      cfgGamma: 'Gamma',
      cfgGammaSub: 'contrast — higher makes the swing more visible',
      cfgFloor: 'Floor',
      cfgFloorSub: 'brightness during silence; 0 = fully dark between words',
      cfgReference: 'Reference',
      cfgReferenceSub: 'speaker level mapped to full brightness — lower = more sensitive',
      cfgCurve: 'Curve',
      cfgCurveSub: 'below 1 lifts quiet consonants into view',
      cfgAdvanced: 'Advanced',
      cfgAdvancedDesc: 'Everything here affects only bounded button-press turns — except the action button setting, which decides whether a tap starts one at all. Wake-word turns stream continuously — Home Assistant\'s VAD endpoints them, and the controller closes accidental wakes after 5s of silence relative to the room\'s measured noise floor — so none of these settings touch the wake path.',
      cfgTapSendsEvent: 'Tap sends an event',
      cfgMultiTapWindow: 'Multi-tap window',
      cfgMultiTapWindowSub: '0 = off. Coalesces quick taps into double/triple, at the cost of delaying every tap by this much. Needs \'Tap sends an event\'',
      cfgConsolePassword: 'Console password',
      cfgConsoleIdleTimeout: 'Console idle timeout',
      cfgAgc: 'Auto gain (AGC)',
      cfgAgcSub: 'levels button-turn speech; never the wake stream',
      cfgThreshold: 'Threshold',
      cfgThresholdSub: 'RMS above this = speech (pre-gain units)',
      cfgSpeechGate: 'Speech gate',
      cfgSpeechGateSub: 'speech needed to open',
      cfgSilenceGate: 'Silence gate',
      cfgSilenceGateSub: 'silence needed to close',
      cfgBluetooth: 'Bluetooth',
      cfgBluetoothDesc: 'Turns the device into a Home Assistant Bluetooth proxy: it passively listens for BLE advertisements (presence beacons, temperature sensors) and forwards them to HA as a separate ESPHome device — independent of the voice assistant. Enabling permanently switches the Dot\'s Bluetooth chip away from Android\'s stack (Bluetooth speaker pairing, never used by Revoice, stops being possible).',
      cfgBluetoothProxy: 'Bluetooth proxy',
      cfgBluetoothProxySub: 'passive BLE scan → HA (Bermuda, BLE sensors)',
      cfgStreaming: 'Streaming',
      cfgStreamingDesc: 'Protocols the Echo speaks for itself, with no controller in the path. Music reaches the speaker straight from the source, so it keeps playing through a controller restart — and it is mixed with voice on the device, so a spoken question ducks it rather than stopping it. A voice request through Home Assistant always wins: the Echo leaves the group and plays what it was asked for, and does not rejoin by itself.',
      cfgSpotifyName: 'Spotify name',
      cfgAirplayName: 'AirPlay name',
      cfgSendspin: 'Sendspin',
      cfgSpotifyConnect: 'Spotify Connect',
      cfgAirplay: 'AirPlay',
      cfgAirplayVolume: 'AirPlay volume moves this Echo',
      cfgAirplayVolumeSub: 'the slider on a phone sets the Echo\'s own volume and flashes the ring, instead of being turned down inside the AirPlay receiver where nothing else can see it. Note this Echo has ONE volume: turn AirPlay down and the assistant\'s next answer is quieter too. Takes effect when AirPlay next starts',
      cfgSpotifyVolume: 'Spotify volume moves this Echo',
      cfgSpotifyVolumeSub: 'the slider in the Spotify app sets the Echo\'s own volume and flashes the ring, instead of being turned down inside librespot where nothing else can see it. Note this Echo has ONE volume: turn Spotify down and the assistant\'s next answer is quieter too. Takes effect when Spotify Connect next starts',

      // -- Configuration, capability reasons --
      cfgActionButton: 'Action button',
      cfgBothCompare: 'Both (compare)',
      cfgDeleteFailed: 'Delete failed',
      cfgUploadFailed: 'Model upload failed',
      cfgTurnProcessing: 'Turn processing',
      cfgUsbConsole: 'USB console',
      cfgMissingFile: 'missing file',
      cfgNotInstalled: 'not installed',
      cfgPerDevice: 'per device',
      cfgNoAirplayRx: 'needs newer firmware on this Echo — it has no AirPlay receiver',
      cfgNoSendspin: 'needs newer firmware on this Echo — it has no Sendspin client',
      cfgNoSpotify: 'needs newer firmware on this Echo — it has no Spotify endpoint',
      cfgNoTapEvent: 'needs newer firmware on this Echo — it has no action-button event for a tap to fire',
      cfgNoOnDevice: 'needs newer firmware on this Echo — the controller listens for now',
      cfgNoHwRef: 'needs newer firmware on this Echo — the software tap is the only source it has',
      cfgFireosFleet: 'every device in this fleet runs FireOS, which uses adb for USB access — this setting would do nothing',
      cfgRefAuto: 'detects the hardware loopback, falls back to the software tap',
      cfgRefHwNote: 'not used — this device has a hardware echo reference',
      cfgRefHw: 'pinned to the playback loopback in the mic capture — no delay to compensate, but a board without one cancels nothing',
      cfgRefSw: 'pinned to the tap at the speaker write — uses the AEC delay above, and re-converges after every volume change',
      cfgAecDelaySub: 'playback write-to-ear latency compensation',
      cfgOnDeviceOn: 'the Echo decides — no network hop before it hears you, and it keeps working through a controller restart. The controller still scores alongside it, so Turns shows whether they agreed',
      cfgOnDeviceShadow: 'the Echo scores alongside the controller and reports what it would have heard, without acting on it — compare in Turns before trusting it',
      cfgSpotifySub: 'the Echo appears in the Spotify app as a speaker and plays from it directly, with no Home Assistant in the path',
      cfgAirplaySub: 'the Echo appears in the AirPlay list and plays from a phone or Mac directly. Classic AirPlay — an AirPlay 2 build has not been made for this hardware yet',
      cfgNameScoped: 'set per device — two Echos announcing the same name make the picker useless',
      cfgAirplayNameSub: 'what THIS Echo is called in the AirPlay list. Blank uses its serial',
      cfgSpotifyNameSub: 'what THIS Echo is called in the Spotify app. Blank uses its serial, which nobody picks out of a list',
      cfgTapEventSub: 'tap fires the HA action-button event instead of starting a turn — hold still fires \'long\'; the button can no longer cancel a response. A tap is easy to trigger by accident and the button is unauthenticated — bind destructive automations to \'long\' instead',
      cfgConsolePasswordSub: 'prompts before the USB serial console hands over a root shell. Applies to emOS devices only — FireOS uses adb. Fleet-wide, and pushed straight to every connected device on save; one that is offline picks it up when it reconnects. Forgetting it costs a reflash, not a device.',
      cfgConsoleTimeoutSub: 'minutes of no typing before the USB console logs out, 0-90. 0 = never. A long command is not interrupted — only an idle prompt.',
      cfgSendspinSub: 'join Music Assistant groups directly — synchronised multi-room, no controller hop. The Echo appears as a speaker in Music Assistant once enabled',

      // -- Configuration, remaining prose --
      cfgDuckDepthSub: 'how far music drops under a voice response — it keeps playing instead of pausing',
      cfgNoMix: 'needs firmware that mixes music and voice (v2.10.0+)',
      cfgHoldoffSub: 'how long the HA "Audio" sensor stays on after the last sound — bridges the gap between an answer and the announcement after it, so an amplifier automated on it does not switch input back and forth',
      cfgNoAudioState: 'needs firmware that reports what its music plane is playing',
      cfgRefDetected: 'detected: using the hardware loopback on this Echo',
      cfgOwwControllerOnly: 'the controller listens; the Echo just streams audio',
      cfgVolumeNote: 'Volume is remembered per device and restored after a reboot. Change it from Home Assistant or the device buttons; the current level is shown on the Status tab.',
      cfgBassGuardNote: 'The speaker cannot reproduce the lowest frequencies, and feeding them to it costs cone movement that muddies everything above. Removing them is what keeps the midrange clean. The change is subtle by design and there is no reason to turn it off.',
      cfgOwwRuntimeNote: 'Needs the wake word runtime installed on this Echo (Updates tab) — costs ~0.4 of a core while it runs.',
      cfgRingMeterNote: 'While a response plays, the ring throbs with the live speaker level. These shape how hard it throbs — the device renders it locally, so changes apply on the next response with no restart. Defaults are tuned for speech; raise Decay and Gamma for a punchier ring, lower them for a calmer one.',
      cfgLibrespotMissing: 'librespot is not installed on this Echo',
      cfgShairportMissing: 'shairport-sync is not installed on this Echo',

      // -- Settings shell --
      settingsTabConfig: 'Config',
      settingsTabUsers: 'Users',
      settingsTabAccount: 'Account',
      settingsTabSupport: 'Support',
      settingsFleetBlurb: 'Default config applied to all devices unless overridden per-device.',
      scopeController: 'Controller',
      scopeSpeaker: 'Speaker',
      scopeDevice: 'Device',
      scopeButtonTurns: 'Button turns only',
      eqPresetFlat: 'Flat',
      eqPresetSpeech: 'Speech',
      eqPresetMusic: 'Music',

      // The fleet in one sentence, assembled. See the note at the top of
      // this file for why this is a function and not a template.
      sentence(s) {
        const w = n => WORDS.en[n] || String(n);
        if (!s.approved) return s.pending ? 'Something new is waiting for you.'
                                          : 'No devices yet.';
        if (!s.online)   return s.approved === 1 ? 'Your device is offline.'
                                                 : 'Every device is offline.';
        const clauses = [];
        if (s.active)  clauses.push([s.active,  'listening']);
        if (s.playing) clauses.push([s.playing, 'playing music']);
        if (!clauses.length) {
          return s.online === 1 ? 'One device is ready.'
                                : `${cap(w(s.online))} devices are ready.`;
        }
        return clauses.map(([n, what], i) => {
          const subject = i === 0 ? (n === 1 ? 'One device' : `${cap(w(n))} devices`)
                                  : w(n);
          return `${subject} ${n === 1 ? 'is' : 'are'} ${what}`;
        }).join(', ') + '.';
      },

      // The exception line under it.
      exception(bits) {
        return bits.length ? bits.join(' · ') : 'Nothing needs attention';
      },
      offlineFor(name, duration) { return `${name} offline ${duration}`; },
      moreOffline(n) { return `${n} more offline`; },
      waitingForApproval(n) { return `${n} waiting for approval`; },
      onOlderFirmware(n) { return `${n} on older firmware`; },
      behindCount(updates, total) {
        return `${updates} of ${total} ${total === 1 ? 'device' : 'devices'} `
             + `${updates === 1 ? 'is' : 'are'} on something older. `
             + 'An update reboots the device.';
      },
      // Inside a sentence rather than as a heading. English lowercases it;
      // German does not, because German capitalises nouns — so this cannot
      // be `t('volume').toLowerCase()`, which is what it was and which
      // rendered "pegel".
      volumeInline: 'volume',

      // "how long ago", against `since` below, which is "for how long".
      // Both exist because they answer different questions and read wrong in
      // each other's places: a device was last seen 10 days AGO and has been
      // offline FOR 14 minutes.
      ago: {
        seconds(n) { return `${n}s ago`; },
        minutes(n) { return `${n}m ago`; },
        hours(n)   { return `${n}h ago`; },
        days(n)    { return `${n}d ago`; },
      },

      since: {
        justNow: 'just now',
        minutes(n) { return `for ${n} minutes`; },
        hours(n)   { return `for ${n} hours`; },
        days(n)    { return `for ${n} days`; },
        unknown:   'for an unknown time',
      },
    },

    de: {
      settings: 'Einstellungen',
      signOut: 'Abmelden',
      controllerVersion: 'Controller',
      reload: 'neu laden',
      themeToLight: 'Zum hellen Modus wechseln',
      themeToDark: 'Zum dunklen Modus wechseln',
      densityToDense: 'Zur dichten Ansicht wechseln',
      densityToRoomy: 'Zur luftigen Ansicht wechseln',
      language: 'Sprache',

      online: 'Online',
      inConversation: 'Im Gespräch',
      playing: 'Wiedergabe',
      waiting: 'Wartet',
      devices: 'Geräte',
      waitingForYou: 'Wartet auf Freigabe',
      nameAndApprove: 'Benennen & freigeben',
      setUpADot: 'Echo Dot einrichten',
      noDevicesYet: 'Noch keine Geräte — schalte einen Revoice-Dot ein, dann erscheint er hier',
      nothingNeedsAttention: 'Nichts zu tun',
      addressUnknown: 'Adresse unbekannt',
      firstSeen: 'zuerst gesehen',
      lastKnownAddress: 'Zuletzt bekannte Adresse',

      device: 'Gerät',
      state: 'Zustand',
      volume: 'Pegel',
      latency: 'Latenz',
      firmware: 'Firmware',
      roundTrip: 'Umlaufzeit der Steuerverbindung',
      updateAvailable: 'Update verfügbar',

      statePending:   'Wartet auf Freigabe',
      stateOffline:   'Offline',
      stateMuted:     'Stumm',
      stateSpeaking:  'Antwortet',
      stateThinking:  'Denkt',
      stateListening: 'Hört zu',
      statePlaying:   'Spielt ab',
      stateIdle:      'Bereit',

      nowPlaying: 'Läuft gerade',
      firmwareSection: 'Firmware',
      checkForUpdates: 'Nach Updates suchen',
      checking: 'Suche…',
      deployToAll: 'Auf alle verteilen',
      everyDeviceLatest: 'Jedes Gerät hat die neueste Version.',
      noReleaseInfo: 'Keine Versionsinformation — der Controller hat GitHub nicht erreicht.',

      playingNow: 'Läuft gerade',
      source: 'Quelle',
      level: 'Pegel',
      ducking: 'Absenkung',
      duckingPauses: 'pausiert',
      playingNoTrackRow: 'Dieses Gerät spielt ab; es meldet nicht, was.',
      playingNoTrackPanel:
        'Dieses Gerät spielt ab. Seine Firmware meldet den Titel nicht, also '
        + 'ist hier noch nichts zu benennen.',

      unnamedDevice: 'Unbenanntes Gerät',
      actionRequired: 'Aktion nötig',
      approvePrompt: 'Benenne das Gerät unten und gib es frei, um es in die Flotte aufzunehmen.',
      approveHeading: 'Was die Freigabe bewirkt, in dieser Reihenfolge:',
      approveConfig: 'Es bekommt die Konfiguration der Flotte.',
      approveSatellite: 'Home Assistant bekommt dafür einen Sprachsatelliten und einen Media-Player.',
      approveEndpoints: 'Seine Endpunkte für Spotify, AirPlay und Sendspin werden erreichbar.',
      approveMic: 'Sein Mikrofon streamt zum Controller, sobald es das Wachwort hört.',

      checkingController: 'Controller wird geprüft…',
      readySignIn: 'Bereit — bitte anmelden',
      firstRunSetup: 'Ersteinrichtung',
      apiUnreachable: 'API nicht erreichbar',
      signingIn: 'Anmeldung läuft…',
      creatingAccount: 'Konto wird angelegt…',
      accountCreated: 'Konto angelegt',
      signedInViaHa: 'Über Home Assistant angemeldet',
      setupToken: 'Einrichtungs-Token',
      username: 'Benutzername',
      password: 'Passwort',
      createAdmin: 'Administrator-Konto anlegen',
      signIn: 'Anmelden',
      tokenInLogs: 'Das Einrichtungs-Token steht im Controller-Log:',
      haDidNotIdentify:
        'Home Assistant hat dich diesem Add-on nicht genannt. Lade das Panel neu '
        + 'oder nimm das Einrichtungs-Token aus dem Add-on-Log.',

      // -- Configuration (DeviceConfigForm) --
      cfgPlayback: 'Wiedergabe',
      cfgPlaybackDesc: 'Antwort-Audio: Home-Assistant-TTS → parametrischer EQ → Resampling → Lautsprecher des Geräts. Die Presets setzen die Fader; zieh einen Fader für eine eigene Kurve.',
      cfgSpeechBoost: 'Sprachanhebung',
      cfgSpeechBoostSub: 'Präsenzanhebung für Stimme',
      cfgDuckDepth: 'Absenkung',
      cfgAudioHoldoff: 'Audio-Nachlauf',
      cfgSpeakerProtection: 'Lautsprecherschutz',
      cfgSpeakerProtectionSub: 'hält Bass, den der Treiber nicht liefern kann, aus den Mitten heraus — eingeschaltet lassen',
      cfgWakeWord: 'Wachwort',
      cfgWakeWordDesc: 'openwakeword bewertet den laufenden Mikrofonstrom auf dem Controller. Die Empfindlichkeit setzt die Erkennungsschwelle — Versuche, die knapp darunter bleiben, zählen als Beinahe-Treffer (Reiter Status).',
      cfgDeleteModel: 'Modell löschen',
      cfgSpeexDenoise: 'Speex-Rauschunterdrückung',
      cfgSpeexDenoiseSub: 'säubert das Audio vor der Bewertung — in lauten Räumen probieren',
      cfgBargeIn: 'Unterbrechen',
      cfgBargeInSub: 'das Wachwort unterbricht die Wiedergabe — vorher AEC einschalten',
      cfgBargeThreshold: 'Unterbrechungsschwelle',
      cfgBargeThresholdSub: 'nötige Wachwort-Sicherheit während der Wiedergabe — anheben, wenn sich eine Antwort selbst abschneidet',
      cfgArbitrationWindow: 'Schiedsfenster',
      cfgArbitrationWindowSub: 'Millisekunden, in denen der Echo, der dich zuerst hört, die anderen stumm stellt — ohne zusätzliche Verzögerung; 0 schaltet es ab',
      cfgWakeDetection: 'Wachwort-Erkennung',
      cfgMicrophones: 'Mikrofone',
      cfgMicrophonesDesc: 'Aufnahme über das 7-Mikrofon-Array. Die Presets steuern, welches Randmikrofon während eines Dialogs benutzt wird — fürs Wachwort hört immer das mittlere. Die Verstärkung hier ist die einzige im Wachwort-Pfad: sie setzt den Pegel, den alles danach zu hören bekommt.',
      cfgMicpga: 'MICPGA',
      cfgMicpgaSub: 'analoge Verstärkung, vor dem ADC',
      cfgDigitalGain: 'Digitale Verstärkung',
      cfgDigitalGainSub: 'digitale Verstärkung im ADC — wirkt auf Wachwort und Dialoge',
      cfgMicGain: 'Mikrofonverstärkung',
      cfgMicGainSub: 'feste Verstärkung auf der 24-Bit-Aufnahme, vor dem 16-Bit-Strom',
      cfgBeamAngle: 'Keulenwinkel',
      cfgBeamAngleSub: '-1 = automatisch (Auswahl nach Einsatzverhältnis)',
      cfgBeamforming: 'Beamforming',
      cfgBeamformingSub: 'Randmikrofon während eines Dialogs festhalten',
      cfgAec: 'Echokompensation (AEC)',
      cfgAecSub: 'zieht die eigene Wiedergabe des Geräts ab — Wachwort und Dialoge',
      cfgNoiseSuppression: 'Rauschunterdrückung',
      cfgNoiseSuppressionSub: 'DTLN-Entrauschung nur auf dem Audio für die Spracherkennung — hilft gegen Lüfter und Brummen, nicht gegen Sprache aus dem Fernseher',
      cfgAecDelay: 'AEC-Verzögerung',
      cfgAecTail: 'AEC-Filterlänge',
      cfgAecTailSub: 'Filterlänge — verbleibender Verzögerungsfehler plus Raumhall',
      cfgEchoReference: 'Echo-Referenz',
      cfgSaveUtterances: 'Sprachaufnahmen sichern',
      cfgSaveUtterancesSub: 'behält das Mikrofon-Audio der letzten 10 Dialoge auf dem Server — unter Dialoge abspielen oder herunterladen',
      cfgRing: 'Ring',
      cfgRingDesc: 'Farben des LED-Rings während eines Gesprächs — der volle Ring beim Zuhören und der Denk-Läufer. Der rote Stummring und der cyanfarbene Lautstärkebogen ändern sich nie; Rot heißt immer, dass die Mikrofone aus sind.',
      cfgDecay: 'Abfall',
      cfgDecaySub: 'wie schnell er fällt — höher zeichnet einzelne Silben nach',
      cfgAttack: 'Anstieg',
      cfgAttackSub: 'wie schnell er bei einer Spitze steigt',
      cfgGamma: 'Gamma',
      cfgGammaSub: 'Kontrast — höher macht den Ausschlag sichtbarer',
      cfgFloor: 'Grundhelligkeit',
      cfgFloorSub: 'Helligkeit bei Stille; 0 = zwischen den Wörtern ganz dunkel',
      cfgReference: 'Bezugspegel',
      cfgReferenceSub: 'Lautsprecherpegel, der voller Helligkeit entspricht — niedriger = empfindlicher',
      cfgCurve: 'Kurve',
      cfgCurveSub: 'unter 1 holt leise Konsonanten ins Bild',
      cfgAdvanced: 'Erweitert',
      cfgAdvancedDesc: 'Alles hier wirkt nur auf begrenzte Dialoge per Tastendruck — außer der Einstellung zur Aktionstaste, die entscheidet, ob ein Tippen überhaupt einen Dialog startet. Dialoge per Wachwort streamen durchgehend: Home Assistants VAD setzt ihr Ende, und der Controller schließt versehentliche Weckrufe nach 5 s Stille gegenüber dem gemessenen Grundrauschen des Raums — keine dieser Einstellungen berührt also den Wachwort-Pfad.',
      cfgTapSendsEvent: 'Tippen sendet ein Ereignis',
      cfgMultiTapWindow: 'Mehrfachtipp-Fenster',
      cfgMultiTapWindowSub: '0 = aus. Fasst schnelle Tipper zu Doppel- und Dreifachtipp zusammen, um den Preis, jeden Tipp um diese Zeit zu verzögern. Braucht „Tippen sendet ein Ereignis“',
      cfgConsolePassword: 'Konsolen-Passwort',
      cfgConsoleIdleTimeout: 'Konsolen-Leerlaufzeit',
      cfgAgc: 'Automatische Aussteuerung (AGC)',
      cfgAgcSub: 'gleicht Sprache in Tastendruck-Dialogen aus; nie den Wachwortstrom',
      cfgThreshold: 'Schwelle',
      cfgThresholdSub: 'RMS darüber gilt als Sprache (Einheiten vor der Verstärkung)',
      cfgSpeechGate: 'Sprach-Tor',
      cfgSpeechGateSub: 'nötige Sprache zum Öffnen',
      cfgSilenceGate: 'Stille-Tor',
      cfgSilenceGateSub: 'nötige Stille zum Schließen',
      cfgBluetooth: 'Bluetooth',
      cfgBluetoothDesc: 'Macht das Gerät zu einem Bluetooth-Proxy für Home Assistant: es hört passiv auf BLE-Aussendungen (Anwesenheits-Beacons, Temperaturfühler) und reicht sie als eigenes ESPHome-Gerät an HA weiter — unabhängig vom Sprachassistenten. Das Einschalten schaltet den Bluetooth-Chip des Dots dauerhaft von Androids Stack weg (Koppeln als Bluetooth-Lautsprecher, von Revoice nie benutzt, ist danach nicht mehr möglich).',
      cfgBluetoothProxy: 'Bluetooth-Proxy',
      cfgBluetoothProxySub: 'passiver BLE-Scan → HA (Bermuda, BLE-Sensoren)',
      cfgStreaming: 'Streaming',
      cfgStreamingDesc: 'Protokolle, die der Echo selbst spricht, ohne Controller dazwischen. Die Musik erreicht den Lautsprecher direkt von der Quelle, läuft also über einen Neustart des Controllers hinweg weiter — und sie wird auf dem Gerät mit der Stimme gemischt, eine gesprochene Frage senkt sie also ab, statt sie anzuhalten. Eine Sprachanfrage über Home Assistant gewinnt immer: der Echo verlässt die Gruppe, spielt das Angefragte, und kehrt nicht von selbst zurück.',
      cfgSpotifyName: 'Spotify-Name',
      cfgAirplayName: 'AirPlay-Name',
      cfgSendspin: 'Sendspin',
      cfgSpotifyConnect: 'Spotify Connect',
      cfgAirplay: 'AirPlay',
      cfgAirplayVolume: 'AirPlay-Lautstärke steuert diesen Echo',
      cfgAirplayVolumeSub: 'der Regler am Telefon setzt die eigene Lautstärke des Echo und lässt den Ring aufleuchten, statt im AirPlay-Empfänger heruntergeregelt zu werden, wo es sonst niemand sieht. Beachte: dieser Echo hat EINE Lautstärke — drehst du AirPlay leiser, ist auch die nächste Antwort des Assistenten leiser. Wirkt, sobald AirPlay das nächste Mal startet',
      cfgSpotifyVolume: 'Spotify-Lautstärke steuert diesen Echo',
      cfgSpotifyVolumeSub: 'der Regler in der Spotify-App setzt die eigene Lautstärke des Echo und lässt den Ring aufleuchten, statt in librespot heruntergeregelt zu werden, wo es sonst niemand sieht. Beachte: dieser Echo hat EINE Lautstärke — drehst du Spotify leiser, ist auch die nächste Antwort des Assistenten leiser. Wirkt, sobald Spotify Connect das nächste Mal startet',

      // -- Configuration, capability reasons --
      cfgActionButton: 'Aktionstaste',
      cfgBothCompare: 'Beide (vergleichen)',
      cfgDeleteFailed: 'Löschen fehlgeschlagen',
      cfgUploadFailed: 'Hochladen des Modells fehlgeschlagen',
      cfgTurnProcessing: 'Dialogverarbeitung',
      cfgUsbConsole: 'USB-Konsole',
      cfgMissingFile: 'Datei fehlt',
      cfgNotInstalled: 'nicht installiert',
      cfgPerDevice: 'pro Gerät',
      cfgNoAirplayRx: 'braucht neuere Firmware auf diesem Echo — er hat keinen AirPlay-Empfänger',
      cfgNoSendspin: 'braucht neuere Firmware auf diesem Echo — er hat keinen Sendspin-Client',
      cfgNoSpotify: 'braucht neuere Firmware auf diesem Echo — er hat keinen Spotify-Endpunkt',
      cfgNoTapEvent: 'braucht neuere Firmware auf diesem Echo — er hat kein Aktionstasten-Ereignis, das ein Tippen auslösen könnte',
      cfgNoOnDevice: 'braucht neuere Firmware auf diesem Echo — vorerst hört der Controller',
      cfgNoHwRef: 'braucht neuere Firmware auf diesem Echo — der Software-Abgriff ist seine einzige Quelle',
      cfgFireosFleet: 'jedes Gerät dieser Flotte läuft auf FireOS, das für den USB-Zugang adb benutzt — diese Einstellung würde nichts bewirken',
      cfgRefAuto: 'erkennt den Hardware-Rückweg und fällt sonst auf den Software-Abgriff zurück',
      cfgRefHwNote: 'nicht benutzt — dieses Gerät hat eine Hardware-Echo-Referenz',
      cfgRefHw: 'fest auf den Wiedergabe-Rückweg in der Mikrofonaufnahme — nichts zu kompensieren, aber ein Board ohne ihn kompensiert gar nichts',
      cfgRefSw: 'fest auf den Abgriff beim Schreiben zum Lautsprecher — nutzt die AEC-Verzögerung oben und konvergiert nach jeder Lautstärkeänderung neu',
      cfgAecDelaySub: 'Ausgleich der Latenz vom Schreiben bis zum Ohr',
      cfgOnDeviceOn: 'der Echo entscheidet — kein Netzwerksprung, bevor er dich hört, und es funktioniert über einen Neustart des Controllers hinweg weiter. Der Controller bewertet weiterhin mit, unter Dialoge siehst du also, ob beide einer Meinung waren',
      cfgOnDeviceShadow: 'der Echo bewertet neben dem Controller mit und meldet, was er gehört hätte, ohne danach zu handeln — vergleiche es unter Dialoge, bevor du dich darauf verlässt',
      cfgSpotifySub: 'der Echo erscheint in der Spotify-App als Lautsprecher und spielt direkt von dort, ohne Home Assistant dazwischen',
      cfgAirplaySub: 'der Echo erscheint in der AirPlay-Liste und spielt direkt von Telefon oder Mac. Klassisches AirPlay — eine AirPlay-2-Fassung ist für diese Hardware noch nicht gebaut worden',
      cfgNameScoped: 'pro Gerät setzen — zwei Echos, die denselben Namen ankündigen, machen die Auswahlliste unbrauchbar',
      cfgAirplayNameSub: 'wie DIESER Echo in der AirPlay-Liste heißt. Leer nimmt seine Seriennummer',
      cfgSpotifyNameSub: 'wie DIESER Echo in der Spotify-App heißt. Leer nimmt seine Seriennummer, die niemand aus einer Liste heraussucht',
      cfgTapEventSub: 'ein Tippen löst das HA-Aktionstasten-Ereignis aus, statt einen Dialog zu starten — Halten löst weiterhin „long“ aus; die Taste kann eine Antwort dann nicht mehr abbrechen. Ein Tippen passiert leicht versehentlich, und die Taste ist nicht authentifiziert — hänge zerstörerische Automatisierungen deshalb an „long“',
      cfgConsolePasswordSub: 'fragt nach, bevor die serielle USB-Konsole eine Root-Shell herausgibt. Gilt nur für emOS-Geräte — FireOS benutzt adb. Flottenweit, und beim Speichern sofort an jedes verbundene Gerät geschickt; ein Gerät, das offline ist, holt es beim nächsten Verbinden nach. Es zu vergessen kostet ein Neuflashen, nicht das Gerät.',
      cfgConsoleTimeoutSub: 'Minuten ohne Tastendruck, bis sich die USB-Konsole abmeldet, 0–90. 0 = nie. Ein langer Befehl wird nicht unterbrochen — nur ein untätiger Prompt.',
      cfgSendspinSub: 'direkt Music-Assistant-Gruppen beitreten — synchronisiertes Multiroom, ohne Umweg über den Controller. Der Echo erscheint nach dem Einschalten als Lautsprecher in Music Assistant',

      // -- Configuration, remaining prose --
      cfgDuckDepthSub: 'wie weit die Musik unter einer Sprachantwort absinkt — sie läuft weiter, statt anzuhalten',
      cfgNoMix: 'braucht Firmware, die Musik und Stimme mischt (v2.10.0+)',
      cfgHoldoffSub: 'wie lange der HA-Sensor „Audio“ nach dem letzten Ton an bleibt — überbrückt die Lücke zwischen einer Antwort und der Ansage danach, damit ein darauf automatisierter Verstärker den Eingang nicht hin- und herschaltet',
      cfgNoAudioState: 'braucht Firmware, die meldet, was ihre Musikebene abspielt',
      cfgRefDetected: 'erkannt: nutzt den Hardware-Rückweg auf diesem Echo',
      cfgOwwControllerOnly: 'der Controller hört zu; der Echo streamt nur Audio',
      cfgVolumeNote: 'Die Lautstärke wird pro Gerät gemerkt und nach einem Neustart wiederhergestellt. Ändern kannst du sie über Home Assistant oder die Tasten am Gerät; der aktuelle Pegel steht im Reiter Status.',
      cfgBassGuardNote: 'Der Lautsprecher kann die tiefsten Frequenzen nicht wiedergeben, und sie hinzuschicken kostet Membranhub, der alles darüber verwäscht. Sie herauszunehmen ist das, was die Mitten sauber hält. Die Änderung ist absichtlich unauffällig, und es gibt keinen Grund, sie abzuschalten.',
      cfgOwwRuntimeNote: 'Braucht die Wachwort-Laufzeit auf diesem Echo (Reiter Updates) — kostet im Betrieb etwa 0,4 eines Kerns.',
      cfgRingMeterNote: 'Während eine Antwort läuft, pulsiert der Ring mit dem aktuellen Lautsprecherpegel. Diese Werte bestimmen, wie stark er pulsiert — das Gerät zeichnet ihn selbst, Änderungen wirken also ab der nächsten Antwort ohne Neustart. Die Vorgaben sind auf Sprache abgestimmt; für einen kräftigeren Ring Abfall und Gamma anheben, für einen ruhigeren senken.',
      cfgLibrespotMissing: 'librespot ist auf diesem Echo nicht installiert',
      cfgShairportMissing: 'shairport-sync ist auf diesem Echo nicht installiert',

      // -- Settings shell --
      settingsTabConfig: 'Konfiguration',
      settingsTabUsers: 'Benutzer',
      settingsTabAccount: 'Konto',
      settingsTabSupport: 'Support',
      settingsFleetBlurb: 'Standardkonfiguration für alle Geräte, sofern sie nicht pro Gerät überschrieben wird.',
      scopeController: 'Controller',
      scopeSpeaker: 'Lautsprecher',
      scopeDevice: 'Gerät',
      scopeButtonTurns: 'nur Tastendialoge',
      eqPresetFlat: 'Neutral',
      eqPresetSpeech: 'Sprache',
      eqPresetMusic: 'Musik',

      sentence(s) {
        const w = n => WORDS.de[n] || String(n);
        if (!s.approved) return s.pending ? 'Etwas Neues wartet auf dich.'
                                          : 'Noch keine Geräte.';
        if (!s.online)   return s.approved === 1 ? 'Dein Gerät ist offline.'
                                                 : 'Alle Geräte sind offline.';
        const clauses = [];
        if (s.active)  clauses.push([s.active,  'hört zu', 'hören zu']);
        if (s.playing) clauses.push([s.playing, 'spielt Musik', 'spielen Musik']);
        if (!clauses.length) {
          return s.online === 1 ? 'Ein Gerät ist bereit.'
                                : `${cap(w(s.online))} Geräte sind bereit.`;
        }
        // German drops the noun in the second clause to a bare pronoun —
        // "eines", "zwei davon" — where English drops it to a number. Same
        // idea, different word, which is exactly why this is not a template.
        return clauses.map(([n, one, many], i) => {
          if (i === 0) {
            return n === 1 ? `Ein Gerät ${one}` : `${cap(w(n))} Geräte ${many}`;
          }
          return n === 1 ? `eines ${one}` : `${w(n)} davon ${many}`;
        }).join(', ') + '.';
      },

      exception(bits) {
        return bits.length ? bits.join(' · ') : 'Nichts zu tun';
      },
      offlineFor(name, duration) { return `${name} offline ${duration}`; },
      moreOffline(n) { return `${n} weitere offline`; },
      waitingForApproval(n) { return `${n} wartet auf Freigabe`; },
      onOlderFirmware(n) { return `${n} auf älterer Firmware`; },
      behindCount(updates, total) {
        return `${updates} von ${total} ${total === 1 ? 'Gerät' : 'Geräten'} `
             + `${updates === 1 ? 'läuft' : 'laufen'} auf einer älteren Version. `
             + 'Ein Update startet das Gerät neu.';
      },
      volumeInline: 'Pegel',

      ago: {
        seconds(n) { return `vor ${n} s`; },
        minutes(n) { return `vor ${n} min`; },
        hours(n)   { return `vor ${n} h`; },
        days(n)    { return `vor ${n} d`; },
      },

      since: {
        justNow: 'gerade eben',
        minutes(n) { return `seit ${n} Minuten`; },
        hours(n)   { return `seit ${n} Stunden`; },
        days(n)    { return `seit ${n} Tagen`; },
        unknown:   'seit unbekannter Zeit',
      },
    },
  };

  const SUPPORTED = ['en', 'de'];

  // The stored choice wins permanently once made, exactly as the theme does:
  // somebody who picks English at their desk does not want it flipping
  // because a different browser reports a different locale.
  function initialLang() {
    try {
      const saved = localStorage.getItem('em-lang');
      if (SUPPORTED.indexOf(saved) >= 0) return saved;
    } catch (e) { /* private mode */ }
    const nav = (global.navigator && (navigator.language || navigator.userLanguage)) || 'en';
    return nav.toLowerCase().indexOf('de') === 0 ? 'de' : 'en';
  }

  let current = initialLang();
  const subs = new Set();

  // A missing key falls back to English rather than rendering `undefined`,
  // because a half-translated build should show the English word and not a
  // gap — the gap is what ships unnoticed.
  function t(key) {
    const v = STRINGS[current][key];
    return v === undefined ? STRINGS.en[key] : v;
  }

  global.EM_I18N = {
    get lang() { return current; },
    supported: SUPPORTED,
    t,
    strings() { return STRINGS[current]; },
    en() { return STRINGS.en; },
    set(lang) {
      if (SUPPORTED.indexOf(lang) < 0 || lang === current) return;
      current = lang;
      try { localStorage.setItem('em-lang', lang); } catch (e) { /* private mode */ }
      try { document.documentElement.setAttribute('lang', lang); } catch (e) {}
      subs.forEach(fn => { try { fn(lang); } catch (e) { console.error(e); } });
    },
    subscribe(fn) { subs.add(fn); return () => subs.delete(fn); },
  };

  try { document.documentElement.setAttribute('lang', current); } catch (e) {}
// `globalThis` rather than `this`: this file is loaded as a classic script
// in both pages, and ALSO imported by tests/fleet_summary.test.mjs, where
// module scope makes `this` undefined. Every DOM and storage access above
// is inside a try/catch for the same reason — off a page there is no
// document, and a ReferenceError is what that looks like.
})(typeof globalThis !== 'undefined' ? globalThis : this);
