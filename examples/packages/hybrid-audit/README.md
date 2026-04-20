# Hybrid Audit Package

This example package shows the Phase 4 hybrid pattern:

- `luc.extension/v1` for long-lived state and sync interception
- `luc.tool/v2` for a hosted tool
- `luc.ui/v1` for a command, inspector tab, and approval policy

Package layout:

```text
hybrid-audit/
  extensions/
    session-audit.yaml
    host.py
  tools/
    session_audit_status.yaml
  ui/
    session-audit.yaml
```

What it does:

- blocks obviously dangerous `bash` requests in `tool.preflight`
- tracks tool and assistant activity in luc-managed storage
- exposes a `session_audit_status` hosted tool
- adds a `Session Audit` inspector tab

To install it as a runtime package, copy the directory into:

```text
<workspace>/.luc/packages/github.com_lutefd_luc-hybrid-audit@v0.1.0/
```
