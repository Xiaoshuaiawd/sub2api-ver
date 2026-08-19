# Message Storage Runtime Toggle Design

## Goal

Allow an administrator to enable or disable request and response body storage from the usage page without restarting the gateway. Disabling storage stops capture for new requests only. Usage logging, billing, and retention cleanup continue normally.

## Behavior

- The settings dialog exposes an `enabled` toggle alongside the 1-30 day retention field.
- The current runtime state is returned by the existing message-storage settings API.
- An update persists both `enabled` and `retention_days` before changing runtime state.
- A disabled state prevents new capture sessions from being created and prevents new storage jobs from being accepted.
- Captures already in progress when the switch is disabled may finish. Existing stored bodies are not deleted by the switch and expire through the existing retention process.
- The static `gateway.message_storage.enabled` setting is the default when no administrator override exists.
- Settings reads that fail at startup fall back to the static configuration. Settings writes fail closed from the administrator's perspective: the runtime value is not changed when persistence fails.
- Settings changes retain the existing administrator audit action.

## Architecture

`MessageStorageService` owns an atomic runtime-enabled value initialized from configuration. It loads an optional database override through `SettingService` during wiring. The capture middleware receives a lightweight enabled provider and checks it once at the start of each request; this avoids database access on the gateway request path.

The administrator API reads and updates the same service. The request and response schema becomes:

```json
{
  "enabled": false,
  "retention_days": 7
}
```

For multiple gateway instances, runtime settings are refreshed from the shared settings table on a bounded interval so a change reaches other instances without placing a database read on each API request. The instance handling the administrator update changes immediately after persistence succeeds.

## UI

The existing settings dialog adds the project's standard accessible toggle above the retention input. The label states that the setting controls request and response body storage. Supporting text explains that disabling it affects new requests only and does not delete historical bodies. The retention input remains editable while storage is disabled.

Saving is a single operation. Loading, disabled, success, and failure states follow the existing dialog and toast patterns.

## Tests

- Service tests cover configuration fallback, persisted override, immediate runtime changes, and persistence failure.
- Middleware tests prove disabled mode does not wrap request/response bodies and enabled mode still captures them.
- Handler tests cover the combined settings payload and validation.
- Frontend tests or type checks cover the updated API types and settings dialog state.
- Existing message capture, storage, repository, and build checks remain green.
