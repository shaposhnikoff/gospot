import { mount } from 'svelte';
import ClassicShell from './ClassicShell.svelte';

const sheet = document.head.querySelector<HTMLLinkElement>('link[data-gospot-classic]');
if (window.top === window && sheet && !document.getElementById('gospot-classic-shell')) {
  // Keep both Svelte's handlers and its search input outside legacy selectors
  // (configure.php and modem_fw_upgrade.php enumerate the document's inputs).
  const host = document.createElement('div');
  host.id = 'gospot-classic-shell';
  const target = host.attachShadow({ mode: 'open' });
  target.append(sheet.cloneNode());
  mount(ClassicShell, { target });
  document.body.prepend(host);
}
