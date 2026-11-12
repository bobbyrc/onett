// Mock only: draws the app shell (the rail, and the phone's top bar) around <main>, so each
// mockup file holds just its page. Reads data-nav ("after-goals" or "after-loops") and data-role
// ("owner" or "viewer") from <body>. The markup and class names are the real ones from
// web/src/components/Shell.tsx, so web/src/styles.css styles them.
(function () {
  var body = document.body;
  var role = body.dataset.role || 'owner';
  var owner = role === 'owner';
  var items = ['Overview', 'Projects', 'Chat', 'Goals', 'Tasks', 'Approvals', 'Runs', 'Deploys', 'Signals', 'Friction', 'Loops', 'Agents', 'Config'];
  if (owner) items.push('Users');
  var after = body.dataset.nav === 'after-loops' ? 'Loops' : 'Goals';
  items.splice(items.indexOf(after) + 1, 0, 'Triggers');
  var badges = {
    Tasks: '<span class="count-badge stop">1<span class="sr-only"> blocked</span></span>',
    Approvals: '<span class="count-badge">1<span class="sr-only"> awaiting approval</span></span>',
  };
  var nav = items
    .map(function (label) {
      return '<a href="#" class="nav-item' + (label === 'Triggers' ? ' active' : '') + '"><span>' + label + '</span>' + (badges[label] || '') + '</a>';
    })
    .join('');
  var mark =
    '<svg class="brand-mark" viewBox="106 106 300 300" aria-hidden="true">' +
    '<path d="M383.5 244.8 A128 128 0 1 1 267.2 128.5" fill="none" stroke="currentColor" stroke-width="44" stroke-linecap="round"/>' +
    '<path d="M329.4 151.1 A128 128 0 0 1 360.9 182.6" fill="none" stroke="var(--lamp-call)" stroke-width="44" stroke-linecap="round"/>' +
    '</svg>';
  var wordmark = '<span class="wordmark">' + mark + 'onett</span>';
  var lamps = [
    ['stop', 'dotfiles-and-agent-configs', '1 blocked'],
    ['stop', 'onett', '1 blocked'],
    ['run', 'home-lab', '1 running'],
    ['off', 'retro-diffusion-sprite-pipeline', 'paused'],
  ]
    .map(function (l) {
      return (
        '<a href="#" class="rail-lamp-row"><span class="rail-lamp-square rail-lamp-' + l[0] + '" aria-hidden="true"></span>' +
        '<span class="rail-lamp-name">' + l[1] + '</span>' +
        '<span class="rail-lamp-label rail-lamp-label-' + l[0] + '">' + l[2] + '</span></a>'
      );
    })
    .join('');
  var account =
    '<div class="account"><div class="account-row"><span class="account-name">' + (owner ? 'bcraig' : 'sam') +
    '<span class="account-role"> ' + role + '</span></span><button type="button" class="link-button">Sign out</button></div>' +
    '<button type="button" class="link-button">Change password</button></div>';
  var pause =
    '<div class="pause-switch"><span class="pause-state"><span class="lamp lamp-run" aria-hidden="true"></span>Running</span>' +
    (owner ? '<button type="button" class="btn btn-secondary pause-button">Pause</button>' : '') +
    '</div>';
  var shell =
    '<header class="topbar">' + wordmark +
    '<span class="topbar-right"><a href="#" class="topbar-approvals"><span class="count-badge">1 approval</span></a>' +
    '<a href="#" class="topbar-approvals"><span class="count-badge stop">1 blocked</span></a>' +
    '<button type="button" class="btn btn-secondary menu-button">Menu</button></span></header>' +
    '<aside class="sidebar"><div class="sidebar-brand">' + wordmark + '</div>' +
    '<nav aria-label="Main">' + nav + '</nav>' +
    '<nav aria-label="Projects" class="rail-lamps">' + lamps + '</nav>' +
    '<div class="sidebar-footer">' + account + pause +
    '<div class="live live-on" role="status"><span class="dot" aria-hidden="true"></span>live</div></div></aside>';
  document.querySelector('.app').insertAdjacentHTML('afterbegin', shell);
  // b-list.html#firings shows the phone's Firings tab.
  if (location.hash === '#firings') {
    body.classList.remove('tab-rules');
    body.classList.add('tab-firings');
    document.querySelectorAll('.trg-tabs button').forEach(function (button, i) {
      button.classList.toggle('on', i === 1);
    });
  }
})();
