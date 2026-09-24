# Client downloads and server import code

The user information page shows **Client downloads and setup** below the greeting. Administrators maintain it under **System → Client download settings**, or through the settings button on the card.

- Add Windows, macOS, Linux and Android entries, with optional architecture and version. Multiple entries per platform are supported.
- Use an HTTP/HTTPS URL or a root-relative site path such as `/downloads/rustdesk.exe`. Blank URLs show “Coming soon”; disabled entries are hidden from users.
- To provide a quick import code, export the server configuration from a configured RustDesk client (Settings → Network → Export Server Config) and paste it into the administrator form. The code is copied verbatim, apart from trimming surrounding whitespace on save. Clearing it disables copying.
- Users copy the code and use Settings → Network → Import Server Config → Apply in RustDesk. This is a clipboard configuration string, not a shell command.
- Refresh or reopen the user page after updating settings.

This release configures links only. It does not upload, fetch or synchronize binaries. Future Git fork synchronization can place files at the configured download paths without changing these entries. The target web server/CDN must actually serve those files; this feature does not create a `/downloads` file server.

## Persistence and access

`client_resources` is a singleton database record. Schema version 267 adds the table to existing installations. Settings remain in the account data volume through rebuilds and restarts; include this volume in backups. No SMTP credentials or runtime ID/relay/API service settings are modified.

- `GET /api/admin/client-resources`: enabled accounts; administrators receive all entries, ordinary users receive enabled entries only.
- `PUT /api/admin/client-resources`: administrator only. Body contains `downloads` and `import_code`.
- Downloads accept at most 24 entries and 2048 bytes per URL; import code accepts at most 16384 bytes. Script/data URLs, protocol-relative URLs and credentials in URLs are rejected.
- The endpoint never fetches remote URLs. Responses use `Cache-Control: private, no-store`.

Validation includes request authorization, invalid update preservation, empty settings, persistence, and migration of an existing installation without changing its user records.
