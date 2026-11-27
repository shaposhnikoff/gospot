import test from 'node:test';
import assert from 'node:assert/strict';
import { dstarVisibility, nbsp, flagStyle, dstarCall, starNetHref, dstarLinkBody, dstarOutput, letters, type DStarInfo, type DStarHeardRow } from './dstar';

const info = (network: boolean): DStarInfo => ({ Network: network, Gateway: null, Links: [], CCS: null, CCSShow: false, StarNet: null, Heard: null, Local: null });

test('D-Star panels appear where index.php includes them', () => {
  // MMDVMHost with the D-Star network: reflector links and manager on the admin page only, CCS on both
  assert.deepEqual(dstarVisibility('mmdvmhost', info(true), false), { gateway: false, links: false, manager: false, ccs: true, heard: false });
  assert.deepEqual(dstarVisibility('mmdvmhost', info(true), true), { gateway: false, links: true, manager: true, ccs: true, heard: false });
  // DStarRepeater: everything on both, the manager on the admin page
  assert.deepEqual(dstarVisibility('dstarrepeater', info(true), false), { gateway: true, links: true, manager: false, ccs: true, heard: true });
  assert.deepEqual(dstarVisibility('dstarrepeater', info(true), true), { gateway: true, links: true, manager: true, ccs: true, heard: true });
  // no D-Star data, no network or no controller: nothing
  const none = { gateway: false, links: false, manager: false, ccs: false, heard: false };
  assert.deepEqual(dstarVisibility('mmdvmhost', null, true), none);
  assert.deepEqual(dstarVisibility('mmdvmhost', info(false), true), none);
  assert.deepEqual(dstarVisibility('none', info(true), true), none);
  assert.deepEqual(dstarVisibility(undefined, info(true), true), none);
});

test('cells keep the classic spacing and colours', () => {
  assert.equal(nbsp('M1ABC  B'), 'M1ABC  B');
  assert.equal(nbsp(''), '');
  assert.equal(nbsp(null), '');
  assert.equal(flagStyle('on'), 'background-color:#1d1;');
  assert.equal(flagStyle('off'), 'background:#606060; color:#b0b0b0;');
  assert.equal(letters.length, 26);
  assert.equal(letters[0] + letters[25], 'AZ');
});

test('last heard callsign links follow last_herd.php', () => {
  const row: DStarHeardRow = { Time: '', Clock: '06:22:00 Nov 27th', Callsign: 'M0XYZ', ID: 'ID51', Lookup: 'M0XYZ', Your: 'CQCQCQ  ', RPT1: '', RPT2: '' };
  assert.deepEqual(dstarCall(row), {
    text: 'M0XYZ', id: 'ID51', href: 'https://database.radioid.net/database/view?callsign=M0XYZ', dprs: 'https://aprs.fi/#!call=M0XYZ*',
  });
  assert.equal(dstarCall(row, 'QRZ').href, 'https://www.qrz.com/db/M0XYZ');
  // hostile RF callsigns are URL-encoded (and rendered as text by Svelte)
  assert.equal(dstarCall({ ...row, Lookup: '<b>&x' }).dprs, 'https://aprs.fi/#!call=%3Cb%3E%26x*');
  assert.equal(starNetHref('2E0ABC'), 'http://www.qrz.com/db/2E0ABC');
});

test('link manager form and messages follow link_manager.php', () => {
  const f = { module: 'M1ABC  B', ref: 'DCS002', text: 'xlx123', custom: false, letter: 'B', action: 'LINK' as const };
  assert.deepEqual(dstarLinkBody(f), { action: 'LINK', module: 'M1ABC  B', refName: 'DCS002', letter: 'B', custom: false });
  assert.deepEqual(dstarLinkBody({ ...f, custom: true }), { action: 'LINK', module: 'M1ABC  B', refName: 'xlx123', letter: 'B', custom: true });
  assert.deepEqual(dstarLinkBody({ ...f, action: 'UNLINK' }).action, 'UNLINK');
  assert.deepEqual(dstarOutput(400, 'Somthing wrong with your input, try again\n'), { text: 'Somthing wrong with your input, try again', table: false });
  assert.deepEqual(dstarOutput(200, 'remotecontrold: command accepted by the gateway/starnetserver'),
    { text: 'remotecontrold: command accepted by the gateway/starnetserver', table: true });
  assert.deepEqual(dstarOutput(409, 'ircDDBGateway Remote Control is not enabled'), { text: 'ircDDBGateway Remote Control is not enabled', table: true });
  assert.deepEqual(dstarOutput(null, ''), { text: 'No response from the board', table: true });
});
