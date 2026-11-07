// Web Serial transport for MeshDevice, replacing @meshtastic/transport-web-serial.
//
// That package can't close the port: on disconnect it tries to cancel a
// stream MeshDevice still holds locked, the cancel fails silently, its own
// reader keeps the port's readable side locked, and port.close() is
// rejected. Reconnecting then fails with "The port is already open".
//
// Here we keep our own reader and writer pipe so disconnect() can release
// both and actually close the port.
import { Types, Utils } from '@meshtastic/core';

type Status = Types.DeviceStatusEnum;

export class SerialTransport implements Types.Transport {
  readonly toDevice: WritableStream<Uint8Array>;
  readonly fromDevice: ReadableStream<Types.DeviceOutput>;

  private readonly abort = new AbortController();
  private readonly writePipe: Promise<void>;
  private reader: ReadableStreamDefaultReader<Types.DeviceOutput> | null = null;
  private out: ReadableStreamDefaultController<Types.DeviceOutput> | null = null;
  private lastStatus: Status | null = null;
  private closing = false;

  constructor(private readonly port: SerialPort) {
    if (!port.readable || !port.writable) throw new Error('The radio port is not open.');

    // App -> radio: frame each packet, then write it to the port.
    const framer = Utils.toDeviceStream();
    this.toDevice = framer.writable;
    this.writePipe = framer.readable
      .pipeTo(port.writable, { signal: this.abort.signal })
      .catch(() => this.lost('write-error'));

    // Radio -> app: split the byte stream into packets and debug text.
    this.reader = port.readable.pipeThrough(Utils.fromDeviceStream()).getReader();
    this.fromDevice = new ReadableStream<Types.DeviceOutput>({
      start: (controller) => {
        this.out = controller;
        this.emit(Types.DeviceStatusEnum.DeviceConnected);
      },
      pull: async (controller) => {
        try {
          const { value, done } = (await this.reader?.read()) ?? { done: true, value: undefined };
          if (done) {
            this.lost('closed');
            controller.close();
          } else controller.enqueue(value);
        } catch {
          this.lost('read-error');
          try {
            controller.close();
          } catch {}
        }
      },
    });

    navigator.serial.addEventListener('disconnect', this.onUnplug);
  }

  /** Stops reading and writing, then closes the port so it can be reopened. */
  async disconnect() {
    if (this.closing) return;
    this.closing = true;
    navigator.serial.removeEventListener('disconnect', this.onUnplug);

    this.abort.abort(); // ends the write pipe and releases port.writable
    try {
      await this.reader?.cancel(); // cancels the read chain and releases port.readable
    } catch {}
    this.reader = null;
    await this.writePipe;

    // The pipes release their locks asynchronously; retry briefly.
    for (let i = 0; i < 20; i++) {
      try {
        await this.port.close();
        break;
      } catch {
        await new Promise((r) => setTimeout(r, 50));
      }
    }
    this.emit(Types.DeviceStatusEnum.DeviceDisconnected, 'user');
  }

  private onUnplug = (e: Event) => {
    if ((e.target as SerialPort | null) === this.port || (e as Event & { port?: SerialPort }).port === this.port) {
      this.lost('serial-disconnected');
    }
  };

  // The radio went away (unplugged, reset, or a read/write failure).
  private lost(reason: string) {
    if (this.closing) return;
    this.emit(Types.DeviceStatusEnum.DeviceDisconnected, reason);
    void this.disconnect();
  }

  private emit(status: Status, reason?: string) {
    if (status === this.lastStatus) return;
    this.lastStatus = status;
    try {
      this.out?.enqueue({ type: 'status', data: { status, reason } });
    } catch {}
  }
}
