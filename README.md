# modem-emu

A high-scale cellular modem emulator for end-to-end testing of Signalroute's `sms-gate` gateway.

Instead of PTYs (limited by the kernel's `/dev/pts` pool), it exposes modems over **TCP sockets** or **Unix domain sockets** — one listener per simulated modem. The gateway uses the `transport: tcp` or `transport: unix` backend introduced in `sms-gate`.

**Scaling**: 10,000 modems = 10,000 goroutines + 10,000 socket listeners. Each goroutine uses ~8 KB of stack and one OS socket. This is well within Linux's default limits.

---

## Architecture

```
sms-gate                             modem-emu
────────                             ─────────
Worker[0]  ──── tcp://127.0.0.1:7000 ──► Listener[0] → Modem[ICCID=...0000]
Worker[1]  ──── tcp://127.0.0.1:7001 ──► Listener[1] → Modem[ICCID=...0001]
...
Worker[N]  ──── tcp://127.0.0.1:700N ──► Listener[N] → Modem[ICCID=...000N]

                                     HTTP :8888
                                     POST /modems/{iccid}/sms/inject
                                     GET  /modems/{iccid}/sms/sent
                                     PUT  /modems/{iccid}/signal
                                     PUT  /modems/{iccid}/registration
                                     POST /scenarios/ban
                                     POST /scenarios/flood
                                     POST /scenarios/fill-storage
```

Each `Listener` accepts one connection at a time (serial port semantics). The `Modem` state machine runs `RunSession(ctx, net.Conn)` — a pure `io.ReadWriteCloser`, fully transport-agnostic.

---

## Quick Start

```bash
# 1. Build
make build

# 2. Run (3 modems on Unix sockets by default)
./go-modem-emu --config configs/config.example.json

# Output:
# ══════════════════════════════════════════════════════════════════
#  Paste into sms-gate config.yaml:
# ══════════════════════════════════════════════════════════════════
# modems:
#   - transport: unix
#     addr: /tmp/modem-emu-0.sock
#     # ICCID: 89490200001234567890  profile: SIM800L
#   - transport: unix
#     addr: /tmp/modem-emu-1.sock
#     # ICCID: 89490200009876543210  profile: SIM7600
# ...
```

```bash
# 3. Start the gateway (paste the config snippet printed above)
./go-sms-gate --config config.yaml
```

```bash
# 4. Inject an incoming SMS — watch it flow to the cloud
curl -X POST http://127.0.0.1:8888/modems/89490200001234567890/sms/inject \
  -H "Content-Type: application/json" \
  -d '{"from":"+4915198765432","body":"Your OTP is 391827"}'

# Response:
# {"slot_index":1,"pdu_hash":"sha256:...","message":"+CMTI URC queued on socket"}
```

---

## TCP transport (for distributed testing)

For testing across machines or containers, use TCP:

```json
{
  "transport": {
    "kind": "tcp",
    "tcp_base_port": 7000,
    "tcp_bind_addr": "0.0.0.0"
  }
}
```

In `sms-gate` config.yaml:
```yaml
modems:
  - transport: tcp
    addr: "emulator-host:7000"
  - transport: tcp
    addr: "emulator-host:7001"
```

---

## Control API

| Method | Path | Body / Query | Description |
|---|---|---|---|
| GET | `/modems` | — | List all modem statuses |
| GET | `/modems/{iccid}` | — | Get one modem |
| POST | `/modems/{iccid}/sms/inject` | `{"from":"...","body":"..."}` | Inject incoming SMS → +CMTI URC |
| GET | `/modems/{iccid}/sms/sent` | — | List SMS the gateway sent |
| DELETE | `/modems/{iccid}/sms/sent` | — | Clear sent history |
| PUT | `/modems/{iccid}/signal` | `{"csq":18}` | Set signal quality (0–31, 99=unknown) |
| PUT | `/modems/{iccid}/registration` | `{"stat":1}` | Set +CREG stat |
| GET | `/modems/{iccid}/storage` | — | SIM storage utilisation |
| POST | `/scenarios/ban` | `?iccid=...` | Set +CREG: 3 (gateway detects SIM_BANNED) |
| POST | `/scenarios/restore` | `?iccid=...` | Restore home registration |
| POST | `/scenarios/weak-signal` | `?iccid=...` | Drop CSQ to 3 (~-107 dBm) |
| POST | `/scenarios/flood` | `?iccid=...&count=N&from=...` | Inject N SMS rapidly |
| POST | `/scenarios/fill-storage` | `?iccid=...` | Fill SIM until full (tests SIM_FULL detection) |

---

## Scale testing

```bash
# Generate a config with 1000 modems (TCP, ports 7000-7999)
python3 scripts/gen_config.py --count 1000 --transport tcp > configs/scale-1000.json

# Start emulator
./go-modem-emu --config configs/scale-1000.json &

# Start gateway with matching config (1000 workers, one per port)
./go-sms-gate --config configs/gateway-scale-1000.yaml
```

---

## Simulated modem profiles

| Profile | Identifies as | `+CREG` format | Notes |
|---|---|---|---|
| `SIM800L` | `SIM800 R14.18` | `0,<stat>` | Classic GPRS module |
| `SIM7600` | `SIM7600E-H` | `2,<stat>` | LTE Cat-4 module |
| `EC21` | `Quectel EC21` | `2,<stat>` | Quectel LTE module |
| `generic` | `go-modem-emu` | `0,<stat>` | Bare minimum |

---

## License

MIT — see [LICENSE](LICENSE).
