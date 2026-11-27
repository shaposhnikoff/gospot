// Display rules of admin/wifi.php: ConvertToChannel and ConvertToSecurity
// from admin/wifi/phpincs.php, the ifconfig byte counters and the input
// checks the page applies before Save (the helper validates again).

const channels: Record<string, string> = {
  '2412': '2.4GHz Ch1', '2417': '2.4GHz Ch2', '2422': '2.4GHz Ch3', '2427': '2.4GHz Ch4', '2432': '2.4GHz Ch5',
  '2437': '2.4GHz Ch6', '2442': '2.4GHz Ch7', '2447': '2.4GHz Ch8', '2452': '2.4GHz Ch9', '2457': '2.4GHz Ch10',
  '2462': '2.4GHz Ch11', '2467': '2.4GHz Ch12', '2472': '2.4GHz Ch13', '2484': '2.4GHz Ch14',
  '504': '5.0GHz Ch8', '506': '5.0GHz Ch12', '508': '5.0GHz Ch16', '517': '5.0GHz Ch34', '518': '5.0GHz Ch36',
  '519': '5.0GHz Ch38', '520': '5.0GHz Ch40', '521': '5.0GHz Ch42', '522': '5.0GHz Ch44', '523': '5.0GHz Ch46',
  '524': '5.0GHz Ch48', '526': '5.0GHz Ch52', '528': '5.0GHz Ch56', '530': '5.0GHz Ch60', '532': '5.0GHz Ch64',
  '550': '5.0GHz Ch100', '552': '5.0GHz Ch104', '554': '5.0GHz Ch108', '556': '5.0GHz Ch112', '558': '5.0GHz Ch116',
  '560': '5.0GHz Ch120', '562': '5.0GHz Ch124', '564': '5.0GHz Ch128', '566': '5.0GHz Ch132', '568': '5.0GHz Ch136',
  '570': '5.0GHz Ch140', '492': '5.0GHz Ch184', '494': '5.0GHz Ch188', '496': '5.0GHz Ch192', '498': '5.0GHz Ch196',
  '5035': '5.0GHz Ch7', '5040': '5.0GHz Ch8', '5045': '5.0GHz Ch9', '5055': '5.0GHz Ch11', '5060': '5.0GHz Ch12',
  '5080': '5.0GHz Ch16', '5170': '5.0GHz Ch34', '5180': '5.0GHz Ch36', '5190': '5.0GHz Ch38', '5200': '5.0GHz Ch40',
  '5210': '5.0GHz Ch42', '5220': '5.0GHz Ch44', '5230': '5.0GHz Ch46', '5240': '5.0GHz Ch48', '5260': '5.0GHz Ch52',
  '5280': '5.0GHz Ch56', '5300': '5.0GHz Ch60', '5320': '5.0GHz Ch64', '5500': '5.0GHz Ch100', '5520': '5.0GHz Ch104',
  '5540': '5.0GHz Ch108', '5560': '5.0GHz Ch112', '5580': '5.0GHz Ch116', '5600': '5.0GHz Ch120', '5620': '5.0GHz Ch124',
  '5640': '5.0GHz Ch128', '5660': '5.0GHz Ch132', '5680': '5.0GHz Ch136', '5700': '5.0GHz Ch140', '5745': '5.0GHz Ch149',
  '5765': '5.0GHz Ch153', '5785': '5.0GHz Ch157', '5805': '5.0GHz Ch161', '5825': '5.0GHz Ch165', '4915': '5.0GHz Ch183',
  '4920': '5.0GHz Ch184', '4925': '5.0GHz Ch185', '4935': '5.0GHz Ch187', '4940': '5.0GHz Ch188', '4945': '5.0GHz Ch189',
  '4960': '5.0GHz Ch192', '4980': '5.0GHz Ch196',
};

/** Frequency in MHz (as iw and wpa_cli report it) to the classic channel label. */
export function convertToChannel(freq: string): string {
  return Object.hasOwn(channels, freq) ? channels[freq] : 'Invalid Channel';
}

const security: Record<string, string> = {
  '[WPA2-PSK-CCMP][ESS]': 'WPA2-PSK (AES)',
  '[WPA2-PSK-CCMP-preauth][ESS]': 'WPA2-PSK (AES) with Preauth',
  '[WPA2-PSK-TKIP][ESS]': 'WPA2-PSK (TKIP)',
  '[WPA2-PSK-CCMP][WPS][ESS]': 'WPA2-PSK (TKIP) with WPS',
  '[WPA-PSK-TKIP+CCMP][WPS][ESS]': 'WPA-PSK (TKIP/AES) with WPS',
  '[WPA-PSK-TKIP][WPA2-PSK-CCMP][WPS][ESS]': 'WPA/WPA2-PSK (TKIP)',
  '[WPA-PSK-TKIP+CCMP][WPA2-PSK-TKIP+CCMP][ESS]': 'WPA/WPA2-PSK (TKIP/AES)',
  '[WPA-EAP-CCMP+TKIP][WPA2-EAP-CCMP+TKIP-preauth][ESS]': 'WPA/WPA2-PSK (TKIP/AES) with Preauth',
  '[WPA-PSK-CCMP+TKIP][WPA2-PSK-CCMP+TKIP][WPS][ESS]': 'WPA/WPA2-PSK (TKIP/AES) with WPS',
  '[WPA-PSK-CCMP][WPA2-PSK-CCMP][WPS][ESS]': 'WPA/WPA2-PSK (AES) with WPS',
  '[WPA-PSK-TKIP][ESS]': 'WPA-PSK (TKIP)',
  '[WEP][ESS]': 'WEP',
  '[ESS]': 'None',
};

/** wpa_cli flags to the classic label; anything else (NetworkManager's "WPA2") as given. */
export function convertToSecurity(flags: string): string {
  return Object.hasOwn(security, flags) ? security[flags] : flags;
}

/** net-tools ifconfig counter: "567890 (554.5 KiB)". */
export function ifconfigBytes(n: number | undefined): string {
  if (n === undefined) return '';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let whole = n, frac = 0, unit = 0;
  for (let i = 0; i < 4; i++) {
    if (whole >= 1024) {
      frac = Math.floor(((whole % 1024) * 10) / 1024);
      whole = Math.floor(whole / 1024);
      unit++;
    }
  }
  return `${n} (${whole}.${frac} ${units[unit]})`;
}

/** 1-32 bytes of UTF-8 without control characters. */
export function ssidOk(ssid: string): boolean {
  const bytes = new TextEncoder().encode(ssid).length;
  return bytes >= 1 && bytes <= 32 && !/[\u0000-\u001f\u007f]/.test(ssid);
}

/** "" keeps the stored key (or makes a new network open); else 8-63 printable ASCII or 64 hex. */
export function pskOk(psk: string): boolean {
  return psk === '' || /^[0-9A-Fa-f]{64}$/.test(psk) || /^[\x20-\x7e]{8,63}$/.test(psk);
}
