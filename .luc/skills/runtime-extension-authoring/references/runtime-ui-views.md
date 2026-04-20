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
~~~

Use this when the user asks for:

- a new inspector tab
- a new inspector panel
- a custom runtime view
- a sidebar-adjacent runtime UI surface

If they ask for "put this in Overview", translate that to one of two paths:

1. runtime extension path: add a new `inspector_tab` or `page`
2. core-code path: only if they explicitly want to alter the built-in `Overview` implementation
