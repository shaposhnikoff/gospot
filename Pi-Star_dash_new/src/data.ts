export type Scenario =
  | 'online'
  | 'master-timeout'
  | 'modem-down'
  | 'idle'
  | 'journal-unavailable'
  | 'api-offline';

export const scenarios: { id: Scenario; label: string }[] = [
  { id: 'online', label: 'Узел на связи' },
  { id: 'master-timeout', label: 'Тайм-аут мастер-сервера' },
  { id: 'modem-down', label: 'Модем не отвечает' },
  { id: 'idle', label: 'Нет передач' },
  { id: 'journal-unavailable', label: 'Журнал недоступен' },
  { id: 'api-offline', label: 'API недоступен' },
];

export type Signal = {
  State: 'up' | 'down' | 'unknown';
  Detail: string;
  Source: string;
  At: string;
};

export type HeardRow = {
  At: string;
  Source: 'RF' | 'Net';
  From: string;
  To: string;
  Slot: string;
  Duration?: number;
  BER?: number;
  RSSI?: number;
  Loss?: number;
};

export type Snapshot = {
  Node: string;
  Modem: Signal;
  Master: Signal;
  Radio: {
    Callsign: string;
    ID: string;
    RX: string;
    TX: string;
    Operation: string;
    CC: string;
    Slots: string;
    Modes: string[];
  };
  Heard: HeardRow[];
  HeardAvailable: boolean;
  UpdatedAt: string;
};

// Observations stay at the same instant across refreshes of this demo session.
const sessionStart = Date.now();
const eventAt = (secondsAgo: number) => new Date(sessionStart - secondsAgo * 1000).toISOString();

function recentCalls(): HeardRow[] {
  return [
    { At: eventAt(46), Source: 'RF', From: 'DEMO-A1', To: 'TG 99', Slot: '2', Duration: 5.4, BER: 0.2 },
    { At: eventAt(81), Source: 'Net', From: 'DEMO-B2', To: 'TG 99', Slot: '2', Duration: 12.1, Loss: 0 },
    { At: eventAt(172), Source: 'RF', From: 'DEMO-A1', To: 'TG 99', Slot: '2', Duration: 8, BER: 0 },
    { At: eventAt(285), Source: 'Net', From: 'DEMO-C3', To: 'TG 91', Slot: '1' },
    { At: eventAt(403), Source: 'RF', From: 'DEMO-D4', To: 'TG 91', Slot: '1', Duration: 17.8, BER: 0.4 },
    { At: eventAt(528), Source: 'Net', From: 'DEMO-C3', To: 'TG 91', Slot: '1', Duration: 9.2, Loss: 0.7 },
  ];
}

export async function fetchSnapshot(scenario: Scenario): Promise<Snapshot> {
  if (scenario === 'api-offline') throw new Error('Не удалось получить снимок: API недоступен.');

  const snapshot: Snapshot = {
    Node: 'pistar-demo',
    Radio: {
      Callsign: 'DEMO-NODE', ID: '0000001', RX: '438.500', TX: '430.900',
      Operation: 'Duplex', CC: '1', Slots: 'TS1 / TS2', Modes: ['DMR'],
    },
    Modem: {
      State: 'up', Detail: 'Модем ответил на запрос MMDVMHost.',
      Source: 'journal: mmdvmhost.service', At: eventAt(3700),
    },
    Master: {
      State: 'up', Detail: 'DMRGateway подтвердил вход на мастер-сервер.',
      Source: 'journal: dmrgateway.service', At: eventAt(3600),
    },
    HeardAvailable: scenario !== 'journal-unavailable',
    Heard: scenario === 'idle' || scenario === 'journal-unavailable' ? [] : recentCalls(),
    UpdatedAt: new Date().toISOString(),
  };

  switch (scenario) {
    case 'master-timeout':
      snapshot.Master = { ...snapshot.Master, State: 'down', Detail: 'Истекло время ожидания ответа мастер-сервера.', At: eventAt(18) };
      break;
    case 'modem-down':
      snapshot.Modem = { ...snapshot.Modem, State: 'down', Detail: 'MMDVMHost не получил ответ от модема.', At: eventAt(14) };
      break;
    case 'journal-unavailable':
      for (const signal of [snapshot.Modem, snapshot.Master]) {
        signal.State = 'unknown';
        signal.Detail = 'Журнал недоступен; состояние не подтверждено.';
        signal.At = snapshot.UpdatedAt;
      }
      break;
  }
  return snapshot;
}
