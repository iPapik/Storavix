# Storavix v0.2.20 — heartbeat independent of Windows CIM collection

## Incident SRV55 (2026-11-24)
- Agent log contained **no** report submission failures, but a 12-minute 8-second gap between successful reports at 05:25:36 and 05:37:44 UTC (plus other long gaps on earlier days).
- The Windows agent previously called collector.Read synchronously before sending every report; a stalled CIM/PowerShell query or scheduling pause could prevent it from contacting Storavix, without producing `report failed`.
- Fixed: host metrics are polled independently of the heartbeat; at most one worker runs. The agent sends a presence report every configured interval even while a host metrics call is slow or stuck. Collected metrics expire after 90 seconds instead of being replayed indefinitely as fresh.
- Added diagnostic log messages for pending/stale/recovered host metrics and slow collection (12s+).
- On the central server, outage and recovery event details now include timestamps and duration, without claiming that Windows was powered off.
- Added tests for a permanently blocked host metrics collector, expiration, and delta calculation.
- Server v0.2.20 accepts existing agents. To fix delayed heartbeats, update the agent on affected endpoints too.

## Limitations
If Windows is fully suspended, frozen or powered off, no application heartbeat can be sent. Those cases still produce a legitimate agent-offline alert. Investigate hypervisor snapshots, backup freezes, clock corrections and Windows event logs when pauses persist.
