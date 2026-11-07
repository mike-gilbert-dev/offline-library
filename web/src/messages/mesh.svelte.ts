// Connection to a Meshtastic radio over USB (Web Serial), plus the nodes and
// messages it reports. One instance lives for the whole page session, so
// leaving the Messages screen does not drop the radio.
//
// Delivery status comes from the radio's routing receipts: an ACK sent by the
// recipient means "delivered", an ACK from our own radio means another node
// rebroadcast it ("relayed"), and an error code means it failed.
//
// History: every message and status change is appended to a per-radio file
// on the drive (see server/messages.go) and replayed when that radio is
// connected again.

// Must come before @meshtastic/core: imports are evaluated in order.
import { installedProcessShim } from './process-shim.js';
import { MeshDevice, Protobuf, Types } from '@meshtastic/core';
import { SerialTransport } from './serial-transport.js';

// The library has finished loading; don't leave a fake Node global behind.
if (installedProcessShim) delete (globalThis as { process?: unknown }).process;

export const BROADCAST = 0xffffffff;
const PORT_ROUTING = 5;

export type ConnState = 'disconnected' | 'connecting' | 'configuring' | 'ready';
// "unconfirmed": sent, but the connection ended before any receipt came back.
export type DeliveryStatus = 'sending' | 'relayed' | 'delivered' | 'failed' | 'unconfirmed';

export interface MeshNode {
  num: number;
  id: string; // "!a0cbd018"
  longName: string;
  shortName: string;
  lastHeard?: Date;
  snr?: number;
  hopsAway?: number;
}

export interface MeshMessage {
  id: number;
  from: number;
  to: number;
  channel: number;
  text: string;
  time: Date;
  outgoing: boolean;
  read: boolean;
  status?: DeliveryStatus;
  error?: string;
}

// Lines in the history file. Names are stored with each message so old
// conversations still show who they were with, even if that radio is gone.
type HistoryRecord =
  | {
      t: 'msg';
      id: number;
      from: number;
      to: number;
      ch: number;
      text: string;
      time: number;
      out: boolean;
      status?: DeliveryStatus;
      peerName?: string;
      peerShort?: string;
    }
  | { t: 'status'; id: number; from: number; status: DeliveryStatus; error?: string };

// USB chips used by Meshtastic boards, so the port picker hides unrelated
// devices: Espressif native USB, Silicon Labs CP210x, WCH CH34x/CH910x,
// Adafruit/RAK nRF52, Seeed, Nordic.
const RADIO_USB_VENDORS = [0x303a, 0x10c4, 0x1a86, 0x239a, 0x2886, 0x1915];

export const nodeId = (num: number) => '!' + (num >>> 0).toString(16).padStart(8, '0');
const hex = (num: number) => nodeId(num).slice(1);

class Mesh {
  readonly supported = typeof navigator !== 'undefined' && 'serial' in navigator;

  state = $state<ConnState>('disconnected');
  error = $state('');
  myNodeNum = $state<number | null>(null);
  firmware = $state('');
  nodes = $state<Record<number, MeshNode>>({});
  messages = $state<MeshMessage[]>([]);
  /** Set when history couldn't be read or saved (for example, drive unplugged). */
  historyProblem = $state('');

  private device: MeshDevice | null = null;
  private transport: SerialTransport | null = null;
  private lastRouting: { requestId: number; from: number } | null = null;
  private pending: { radio: string; rec: HistoryRecord }[] = [];
  private flushTimer: ReturnType<typeof setTimeout> | null = null;

  get me(): MeshNode | undefined {
    return this.myNodeNum === null ? undefined : this.nodes[this.myNodeNum];
  }

  async connect() {
    if (!this.supported || this.state !== 'disconnected') return;
    this.error = '';
    let port: SerialPort;
    try {
      port = await navigator.serial.requestPort({
        filters: RADIO_USB_VENDORS.map((usbVendorId) => ({ usbVendorId })),
      });
    } catch {
      return; // picker closed without choosing a radio
    }
    this.state = 'connecting';
    try {
      await port.open({ baudRate: 115200 });
      // ESP32-S3 radios with built-in USB send nothing until the host raises
      // DTR ("terminal ready").
      await port.setSignals({ dataTerminalReady: true });
      const transport = new SerialTransport(port);
      this.transport = transport;
      const device = new MeshDevice(transport);
      quietLogger(device);
      this.device = device;
      this.subscribe(device);
      device.setHeartbeatInterval(60_000); // serial clients time out after 15 min of silence
      await device.configure();
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      await this.disconnect();
      try {
        await port.close(); // in case it opened but the transport never started
      } catch {}
    }
  }

  async disconnect() {
    const device = this.device;
    const transport = this.transport;
    this.device = null;
    this.transport = null;
    this.state = 'disconnected';
    this.markUnconfirmed();
    try {
      // Stops the heartbeat and send queue, then closes the transport.
      await device?.disconnect();
    } catch {
      // device.disconnect() closes its outgoing stream first, which can fail
      // mid-send; make sure the port is released regardless.
    }
    await transport?.disconnect();
  }

  /** Sends a direct message. Returns once it is queued; status updates follow. */
  async sendDirect(to: number, text: string) {
    const device = this.device;
    if (!device || this.state !== 'ready' || to === BROADCAST) return;
    // sendText echoes the outgoing packet through onMessagePacket (adding it
    // to `messages`) before resolving on the delivery receipt.
    try {
      await device.sendText(text, to, true, Types.ChannelNumber.Primary);
    } catch (e) {
      // Queue timeouts and routing errors arrive here as { id, error }.
      const err = e as { id?: number; error?: number };
      const msg = err.id !== undefined ? this.messages.find((m) => m.id === err.id && m.outgoing) : undefined;
      if (msg && msg.status === 'sending') {
        this.setStatus(msg, 'failed', routingError(err.error) ?? (e instanceof Error ? e.message : 'Not delivered'));
      }
    }
  }

  // ---- radio events -------------------------------------------------------

  private subscribe(device: MeshDevice) {
    const ev = device.events;

    ev.onDeviceStatus.subscribe((s) => {
      const S = Types.DeviceStatusEnum;
      if (s === S.DeviceConfigured) this.state = 'ready';
      else if (s === S.DeviceConfiguring) this.state = 'configuring';
      else if (s === S.DeviceConnecting || s === S.DeviceReconnecting) this.state = 'connecting';
      else if (s === S.DeviceDisconnected && this.device === device) {
        // Unplugged or lost; the transport has already released the port.
        this.device = null;
        this.transport = null;
        this.state = 'disconnected';
        this.error ||= 'The radio was disconnected.';
        this.markUnconfirmed();
      }
    });

    // The radio identifies itself first. A different radio than last time
    // gets its own nodes and history.
    ev.onMyNodeInfo.subscribe((info) => {
      if (info.myNodeNum !== this.myNodeNum) {
        this.myNodeNum = info.myNodeNum;
        this.nodes = {};
        this.messages = [];
      }
      void this.loadHistory(info.myNodeNum);
    });

    ev.onDeviceMetadataPacket.subscribe((p) => (this.firmware = p.data.firmwareVersion));

    ev.onNodeInfoPacket.subscribe((n) => {
      this.upsertNode(n.num, {
        longName: n.user?.longName,
        shortName: n.user?.shortName,
        lastHeard: n.lastHeard ? new Date(n.lastHeard * 1000) : undefined,
        snr: n.snr || undefined,
        hopsAway: n.hopsAway,
      });
    });

    ev.onUserPacket.subscribe((p) => {
      this.upsertNode(p.from, { longName: p.data.longName, shortName: p.data.shortName });
    });

    ev.onMeshPacket.subscribe((p) => {
      if (p.from && p.from !== this.myNodeNum && p.rxTime) {
        this.upsertNode(p.from, { lastHeard: new Date(p.rxTime * 1000), snr: p.rxSnr || undefined });
      }
      // Routing receipts carry the original packet id in requestId, which the
      // library drops from onRoutingPacket; remember it for the handler below.
      const d = p.payloadVariant.case === 'decoded' ? p.payloadVariant.value : null;
      this.lastRouting = d && d.portnum === PORT_ROUTING ? { requestId: d.requestId, from: p.from } : null;
    });

    ev.onRoutingPacket.subscribe((p) => {
      const ctx = this.lastRouting;
      if (!ctx || p.data.variant.case !== 'errorReason') return;
      const msg = this.messages.find((m) => m.id === ctx.requestId && m.outgoing);
      if (!msg) return;
      const code = p.data.variant.value as number;
      if (code === 0) {
        if (ctx.from === msg.to) this.setStatus(msg, 'delivered');
        else if (msg.status === 'sending') this.setStatus(msg, 'relayed');
      } else if (msg.status !== 'delivered') {
        this.setStatus(msg, 'failed', routingError(code));
      }
    });

    ev.onMessagePacket.subscribe((p) => {
      if (this.messages.some((m) => m.id === p.id && m.from === p.from)) return; // duplicate
      const outgoing = p.from === this.myNodeNum;
      const msg: MeshMessage = {
        id: p.id,
        from: p.from,
        to: p.to,
        channel: p.channel,
        text: p.data,
        time: isNaN(p.rxTime.getTime()) || p.rxTime.getTime() === 0 ? new Date() : p.rxTime,
        outgoing,
        read: outgoing,
        status: outgoing ? 'sending' : undefined,
      };
      this.messages.push(msg);
      const peer = msg.to === BROADCAST ? undefined : this.nodes[outgoing ? msg.to : msg.from];
      this.save({
        t: 'msg',
        id: msg.id,
        from: msg.from,
        to: msg.to,
        ch: msg.channel,
        text: msg.text,
        time: msg.time.getTime(),
        out: outgoing,
        status: msg.status,
        peerName: peer?.longName !== peer?.id ? peer?.longName : undefined,
        peerShort: peer?.longName !== peer?.id ? peer?.shortName : undefined,
      });
    });
  }

  private setStatus(msg: MeshMessage, status: DeliveryStatus, error?: string) {
    if (msg.status === status && msg.error === error) return;
    msg.status = status;
    msg.error = error;
    this.save({ t: 'status', id: msg.id, from: msg.from, status, ...(error ? { error } : {}) });
  }

  // Anything still "sending" when the connection ends will never get a receipt.
  private markUnconfirmed() {
    for (const m of this.messages) if (m.status === 'sending') this.setStatus(m, 'unconfirmed');
  }

  private upsertNode(num: number, patch: Partial<Omit<MeshNode, 'num' | 'id'>>) {
    const existing = this.nodes[num];
    const clean = Object.fromEntries(Object.entries(patch).filter(([, v]) => v !== undefined && v !== ''));
    if (existing) Object.assign(existing, clean);
    else {
      const id = nodeId(num);
      // Until the radio's name arrives, show its ID. (Not "Meshtastic xxxx":
      // that is the firmware's real default name and would be ambiguous.)
      this.nodes[num] = { num, id, longName: id, shortName: id.slice(-4), ...clean };
    }
  }

  // ---- history on the drive -------------------------------------------------

  private async loadHistory(radioNum: number) {
    let records: HistoryRecord[];
    try {
      const res = await fetch(`/api/messages/${hex(radioNum)}`);
      if (!res.ok) throw new Error(`${res.status}`);
      records = await res.json();
    } catch {
      this.historyProblem = "Earlier messages couldn't be loaded from the drive.";
      return;
    }
    if (this.myNodeNum !== radioNum) return; // a different radio was connected meanwhile
    this.historyProblem = '';

    const key = (id: number, from: number) => `${from}:${id}`;
    const byKey = new Map(this.messages.map((m) => [key(m.id, m.from), m]));
    const loaded: MeshMessage[] = [];
    for (const r of records) {
      if (r.t === 'msg') {
        if (byKey.has(key(r.id, r.from))) continue;
        const m: MeshMessage = {
          id: r.id,
          from: r.from,
          to: r.to,
          channel: r.ch,
          text: r.text,
          time: new Date(r.time),
          outgoing: r.out,
          read: true,
          status: r.status,
        };
        byKey.set(key(r.id, r.from), m);
        loaded.push(m);
        const peer = r.to === BROADCAST ? undefined : r.out ? r.to : r.from;
        if (peer !== undefined && !this.nodes[peer]) {
          this.upsertNode(peer, { longName: r.peerName, shortName: r.peerShort });
        }
      } else if (r.t === 'status') {
        const m = byKey.get(key(r.id, r.from));
        // Only update messages that came from the file; live ones are newer.
        if (m && loaded.includes(m)) {
          m.status = r.status;
          m.error = r.error;
        }
      }
    }
    for (const m of loaded) if (m.status === 'sending') m.status = 'unconfirmed';
    this.messages = [...loaded, ...this.messages].sort((a, b) => a.time.getTime() - b.time.getTime());
  }

  private save(rec: HistoryRecord) {
    if (this.myNodeNum === null) return;
    this.pending.push({ radio: hex(this.myNodeNum), rec });
    this.scheduleFlush(300);
  }

  private scheduleFlush(ms: number) {
    if (this.flushTimer === null) this.flushTimer = setTimeout(() => void this.flush(), ms);
  }

  // Writes queued records in batches. If the drive is unavailable they stay
  // queued and are retried, so nothing is lost while it reconnects.
  private async flush() {
    this.flushTimer = null;
    const batch = this.pending.splice(0);
    const byRadio = new Map<string, HistoryRecord[]>();
    for (const { radio, rec } of batch) byRadio.set(radio, [...(byRadio.get(radio) ?? []), rec]);
    for (const [radio, recs] of byRadio) {
      try {
        const res = await fetch(`/api/messages/${radio}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(recs),
        });
        if (!res.ok) throw new Error(`${res.status}`);
        this.historyProblem = '';
      } catch {
        this.pending.unshift(...recs.map((rec) => ({ radio, rec })));
        this.historyProblem = "Messages aren't being saved to the drive right now. They'll be saved when it's available.";
      }
    }
    if (this.pending.length) this.scheduleFlush(5000);
  }
}

// The library's built-in logger is tslog's Node.js build (see
// process-shim.ts): logging an Error through it touches Buffer and
// process.cwd, which would throw inside the radio stream. The library only
// calls trace/debug/info/warn/error/fatal, so swap in a console logger.
function quietLogger(device: MeshDevice) {
  const warn = (...args: unknown[]) => console.warn('[meshtastic]', ...args);
  const error = (...args: unknown[]) => console.error('[meshtastic]', ...args);
  const ignore = () => undefined;
  device.log = { trace: ignore, debug: ignore, info: ignore, warn, error, fatal: error } as unknown as MeshDevice['log'];
}

function routingError(code: number | undefined): string | undefined {
  if (code === undefined) return undefined;
  const names: Record<number, string> = {
    [Protobuf.Mesh.Routing_Error.NO_ROUTE]: 'No route to that radio',
    [Protobuf.Mesh.Routing_Error.MAX_RETRANSMIT]: 'No reply after several tries',
    [Protobuf.Mesh.Routing_Error.TIMEOUT]: 'Timed out',
    [Protobuf.Mesh.Routing_Error.NO_CHANNEL]: 'Channel mismatch',
    [Protobuf.Mesh.Routing_Error.TOO_LARGE]: 'Message too long',
    [Protobuf.Mesh.Routing_Error.NO_RESPONSE]: 'No response',
    [Protobuf.Mesh.Routing_Error.DUTY_CYCLE_LIMIT]: 'Radio airtime limit reached; try again shortly',
    [Protobuf.Mesh.Routing_Error.PKI_FAILED]: 'Encryption failed',
    [Protobuf.Mesh.Routing_Error.PKI_UNKNOWN_PUBKEY]: "That radio's encryption key is unknown",
  };
  return names[code] ?? `Not delivered (${Protobuf.Mesh.Routing_Error[code] ?? code})`;
}

export const mesh = new Mesh();
