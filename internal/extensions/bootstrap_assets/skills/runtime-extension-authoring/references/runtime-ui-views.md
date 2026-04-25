# Runtime UI Views

Yes: luc supports creating brand-new runtime views with `luc.ui/v1`.

Supported placements in this slice:

- `inspector_tab` for a new tab in the inspector
- `page` for a dedicated runtime page

Important limit:

- You can add a new runtime view such as `activity.summary`.
- You cannot inject a new field directly into the built-in `Overview` tab through runtime manifests.

Concrete inspector-tab example:

~~~yaml
schema: luc.ui/v1
id: activity-ui
commands:
  - id: activity.summary.open
    name: Open activity summary
    action:
      kind: view.open
      view_id: activity.summary
views:
  - id: activity.summary
    title: Activity Summary
    placement: inspector_tab
    source_tool: activity_summary
    render: json
    actions:
      - id: refresh
        label: Refresh
        action:
          kind: view.refresh
          view_id: activity.summary
      - id: reset
        label: Reset
        shortcut: r
        action:
          kind: tool.run
          tool_name: activity_reset
          result:
            presentation: status
~~~

Runtime view actions:

- Declare `actions[]` on a runtime view when users should act from the same surface where they inspect state.
- luc renders view actions as native selectable rows in inspector tabs and pages.
- Users can move with tab/arrows, press `enter`, or use an action `shortcut`.
- Supported action kinds are `tool.run`, `view.refresh`, `command.run`, `modal.open`, `confirm.request`, and `view.open`.
- Keep view content declarative; actions trigger host-owned behavior and do not inject custom UI components.

Use this when the user asks for:

- a new inspector tab
- a new inspector panel
- a custom runtime view
- a sidebar-adjacent runtime UI surface

If they ask for "put this in Overview", translate that to one of two paths:

1. runtime extension path: add a new `inspector_tab` or `page`
2. core-code path: only if they explicitly want to alter the built-in `Overview` implementation
