// Fixtures for the dashboard harness — see README.md.
//
// The real bundle talks to /api over fetch and opens a WebSocket. Both are
// stubbed with fixtures here so the page renders a plausible fleet: one
// device listening, one playing over Spotify, one offline, one MUTED and
// one pending.
//
// The muted device sits next to the offline one deliberately. They are the
// two states drawn in red, one of them is not a fault, and whether they can
// be told apart is a question only a screenshot answers.
(function () {
  try {
    localStorage.setItem('em_token', 'harness');
    localStorage.setItem('em_role', 'admin');
    localStorage.removeItem('em_auth_via');
  } catch (e) {}

  // Fixtures must carry the API's OWN shapes, not plausible-looking ones.
  // `volume` is the 0.0–1.0 float em_volume.device_level_to_ha produces; it
  // was written here as 0-100 at first and every row rendered beautifully
  // while being wrong by a factor of a hundred on real data.
  const now = Math.floor(Date.now() / 1000);
  const base = (id, label, extra) => Object.assign({
    device_id: id, label, approved: true, connected: true,
    ip: '192.168.1.' + (40 + label.length), firmware_ver: 'v2.40.0-fx.1',
    first_seen: now - 900000, last_seen: now - 12,
    config: {}, config_sections: [], use_global_config: true,
    esphome_port: 16001, ble_proxy_port: 17001,
    speaking: false, muted: false, listening: false, thinking: false,
    stats: null, rttMs: 7, volume: 0.42, bleProxy: null, voiceSatellite: null,
    linkDown: false, linkTokenIssued: true, linkTls: true, owwNearMisses: 0,
    owwShadowCapable: true, owwTriggerCapable: true, audioMixCapable: true,
    audioStateCapable: true, audio: { active: false, source: 'none' },
    aecHwRefCapable: true, aecRef: 'hw', endpointHealthCapable: true,
    endpointHealth: null, baseOs: 'emos', androidUserspace: false,
    buttonHoldCapable: true, sendspinCapable: true,
    spotifyCapable: true, spotifyStatus: 'installed',
    airplayCapable: true, airplayStatus: 'installed',
    ambientLightCapable: true, wifi: null,
    update_in_progress: false, update_queued: false, update_error: null,
  }, extra || {});

  const DEVICES = [
    base('G090LF1180570SPJ', 'Kitchen', { listening: true, rttMs: 6, volume: 0.55 }),
    base('G090LF1180570ABC', 'Lounge', {
      audio: { active: true, source: 'spotify' }, volume: 0.31, rttMs: 11,
    }),
    base('G090LF1180570DEF', 'Office', {
      connected: false, last_seen: now - 14 * 60, rttMs: null, volume: 0.25,
      firmware_ver: 'v2.38.0-fx.1',
    }),
    base('G090LF1180570GHI', 'Bedroom', {
      muted: true, volume: 0.18, rttMs: 9,
    }),
    base('G090LF1180570XYZ', 'G090LF1180570XYZ', {
      approved: false, label: null, firmware_ver: null, rttMs: null, volume: null,
    }),
  ];

  // The emOS panel's answer, which is a SUB-path of /api/devices and would
  // otherwise be served the device list by the prefix match below — the panel
  // would then read `.diag` off an array and render nothing, which is exactly
  // the state a harness is supposed to make visible rather than imitate.
  const EMOS = {
    connected: true, baseOs: 'emos', current: '0.6.0-fx.1',
    latest: '0.6.0-fx.1', comparable: true, available: false,
    goodImage: true, freeMb: 334, eligible: true, reason: null,
    reasonText: null,
    diagSummary: 'dns_no_socket',
    diag: {
      dnsSocket: false, dns: 'no_socket',
      dnsDetail: "ping: bad address 'apresolve.spotify.com'",
      ports: [5000, 36000], airplayListening: true, airplay2Listening: false,
      spotifyListening: true, ap2Installed: true, classicInstalled: true,
      nqptpInstalled: false, nqptpRunning: null,
      netlog: ['[  12345] wlan0 up 192.168.178.140',
               '[  12900] dnsproxyd: could not bind /dev/socket/dnsproxyd'],
      airplayWanted: true, airplay2Wanted: true, spotifyWanted: true,
    },
  };

  const ROUTES = {
    '/api/devices': DEVICES,
    '/api/system/status': {
      controller_version: '2.46.0-fx.1', bundle_version: 'harness',
      fleet_base_os: ['emos'], ha_ingress: false, local_time: '2026-09-13 16:40',
    },
    '/api/releases/latest': { version: 'v2.40.0-fx.1', url: '#' },
    '/api/releases/controller': { available: false },
    '/api/global/config': {},
    '/api/auth/me': { role: 'admin', username: 'felix' },
  };

  const realFetch = window.fetch.bind(window);
  window.fetch = function (input, init) {
    const url = typeof input === 'string' ? input : input.url;
    const path = '/' + String(url).replace(/^.*?\/(api\/)/, '$1');
    // Checked before the prefix loop, for the reason EMOS is declared above.
    if (path.endsWith('/emos')) {
      return Promise.resolve(new Response(JSON.stringify(EMOS), {
        status: 200, headers: { 'Content-Type': 'application/json' },
      }));
    }
    for (const key of Object.keys(ROUTES)) {
      if (path.startsWith(key)) {
        return Promise.resolve(new Response(JSON.stringify(ROUTES[key]), {
          status: 200, headers: { 'Content-Type': 'application/json' },
        }));
      }
    }
    if (String(url).startsWith('http')) return realFetch(input, init);
    return Promise.resolve(new Response('{}', {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }));
  };

  // The events socket never connects here; the poll fallback carries the page.
  window.WebSocket = function () {
    this.close = function () {};
    this.send = function () {};
  };
})();
