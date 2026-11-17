// Mock only. Draws the app shell (the rail, and the phone's top bar) around <main>, so each mockup
// file holds just its page, and shows one state of the file: the <section data-state> named by
// the URL hash (a.html#review), or the first one. A section's data-lamps lists the projects on the
// rail as "name|lamp|words;...". The markup and class names are the real ones from
// web/src/components/Shell.tsx, so web/src/styles.css styles them.
(function () {
  var sections = Array.prototype.slice.call(document.querySelectorAll('section[data-state]'));
  var want = location.hash.slice(1);
  var shown = sections.filter(function (s) { return s.dataset.state === want; })[0] || sections[0];
  sections.forEach(function (s) { if (s !== shown) s.remove(); });
  document.title += ': ' + shown.dataset.state;

  var active = shown.dataset.active || 'Projects';
  var items = ['Overview', 'Projects', 'Chat', 'Goals', 'Tasks', 'Approvals', 'Runs', 'Deploys', 'Signals', 'Friction', 'Loops', 'Triggers', 'Agents', 'Config', 'Users'];
  var nav = items
    .map(function (label) {
      return '<a href="#" class="nav-item' + (label === active ? ' active' : '') + '"><span>' + label + '</span></a>';
    })
    .join('');
  var mark =
    '<svg class="brand-mark" viewBox="106 106 300 300" aria-hidden="true">' +
    '<path d="M383.5 244.8 A128 128 0 1 1 267.2 128.5" fill="none" stroke="currentColor" stroke-width="44" stroke-linecap="round"/>' +
    '<path d="M329.4 151.1 A128 128 0 0 1 360.9 182.6" fill="none" stroke="var(--lamp-call)" stroke-width="44" stroke-linecap="round"/>' +
    '</svg>';
  var wordmark = '<span class="wordmark">' + mark + 'onett</span>';
  var lamps = (shown.dataset.lamps || '')
    .split(';')
    .filter(Boolean)
    .map(function (row) {
      var l = row.split('|');
      return (
        '<a href="#" class="rail-lamp-row"><span class="rail-lamp-square rail-lamp-' + l[1] + '" aria-hidden="true"></span>' +
        '<span class="rail-lamp-name">' + l[0] + '</span>' +
        '<span class="rail-lamp-label rail-lamp-label-' + l[1] + '">' + l[2] + '</span></a>'
      );
    })
    .join('');
  var account =
    '<div class="account"><div class="account-row"><span class="account-name">bcraig<span class="account-role"> owner</span></span>' +
    '<button type="button" class="link-button">Sign out</button></div>' +
    '<button type="button" class="link-button">Change password</button></div>';
  var pause =
    '<div class="pause-switch"><span class="pause-state"><span class="lamp lamp-run" aria-hidden="true"></span>Running</span>' +
    '<button type="button" class="btn btn-secondary pause-button">Pause</button></div>';
  var shell =
    '<header class="topbar">' + wordmark +
    '<span class="topbar-right"><button type="button" class="btn btn-secondary menu-button">Menu</button></span></header>' +
    '<aside class="sidebar"><div class="sidebar-brand">' + wordmark + '</div>' +
    '<nav aria-label="Main">' + nav + '</nav>' +
    '<nav aria-label="Projects" class="rail-lamps">' + lamps + '</nav>' +
    '<div class="sidebar-footer">' + account + pause +
    '<div class="live live-on" role="status"><span class="dot" aria-hidden="true"></span>live</div></div></aside>';
  document.querySelector('.app').insertAdjacentHTML('afterbegin', shell);
})();
