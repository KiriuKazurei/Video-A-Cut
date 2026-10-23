# vac-draw-remotion

Instruction-only Remotion composition skill for VAC route 2 (绘制).
Phase 1 does not render. Scaffold, render, sandbox install, and import are 方案接线中.
Do not invoke a shell or install dependencies. Do not expose a shell to the frontend.
A React component tree is the composition model. This package does not start a renderer.
Keep composition source until a DRAW01 worker exists. An MP4 is not a reversible composition source.
Handoffs, typed by the draw page and not executed here:
- import_into_editing: after render, register the product as a project resource and place it on the editing timeline.
- composite_preview: an unrendered composition may preview together with edit tracks. No MP4 is required.
- merge_render: multiple draw slots and edit tracks render together at the end. Do not force a per-slot export first.
video-use may delegate here. This package does not replace that route-2 entry.
