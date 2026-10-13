# gost-plugin App TDD evidence

Base: `7ea3b5b7cca688f1cf2aba7f6fdd0bcc6809471a`

## RED

- Parser/round-trip tests: `01-parser/red.log`
  - Initial environment prerequisites were restored locally (ignored AAR/assets/JNI placeholder) before the compile/test RED was captured.
  - RED then failed on missing `GostPluginOptions`, proving the codec/round-trip test preceded production code.
- Structured UI: `02-ui/red.log`
  - Failed compilation on missing `GostPluginUiState` and `configureGostPluginPreferences`.

## GREEN

- Parser, SIP003 escaping, YAML map/string forms, Bean/Kryo/clone/URI/JSON/native outbound and UoT v2: `01-parser/green.log` — PASS.
- Structured UI with API 28/31 Robolectric lifecycle and non-gost isolation: `02-ui/green.log` — PASS.

## Regression

- `03-regression/targeted.log`: gost-plugin, Shadowsocks and adjacent Snell YAML tests — PASS.
- `03-regression/full-app.log`: full `:app:testOssDebugUnitTest` — PASS.

All fixtures use `.invalid`, RFC documentation addresses, and test-only passwords.
