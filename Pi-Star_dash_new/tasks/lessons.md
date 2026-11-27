# Preview navigation

- A successful operator HTTP response does not prove management navigation. The first preview advertised port 8788 without starting its management backend; that port belonged to another application.
- Use explicit runtime ports, isolate the preview backend, and verify actual catalog-to-editor-to-operator transitions in Chromium before sharing a preview link. Never stop an unrelated listener to make a hardcoded link work.
- A forwarded preview origin is not the server's listen address. Use one browser origin for all preview navigation and API requests; validate a forwarded Host and real saved data rather than only inspecting link hostnames.

- 2026-11-27: Quick Edit applied the classic "empty -> none" write rule to every submitted key, so saving an unchanged form rewrote `[D-Star] WhiteList=` to `none` on pistar3 and restarted MMDVMHost. Fixed: only fields whose value differs from the displayed one are written. Restored the file byte-for-byte from the migration backup. Lesson: test a no-op save against a fake helper before calling a real write path on a board, and assert "unchanged form => no changes, no restart".

## 2026-11-27: Configuration — untouched form must not write

- Classic appliers stage defaults and normalisation on every save; on a minimal or
  older INI an untouched Apply then rewrote dozens of keys (found by the stand e2e).
- Rule: plan = changes for the posted form minus changes for the same form with
  every field as displayed. Keep `TestConfigureUntouchedRoundTrip` green for the
  stand fixture, a classic fixture and (via PISTAR_ROUNDTRIP_DIR) real board files.
- Section unit tests use `planCfgRaw`; the edit-only rule is tested on `planCfgApply`.
