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
