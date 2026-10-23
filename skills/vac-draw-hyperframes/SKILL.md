# vac-draw-hyperframes

Instruction-only HyperFrames composition skill for VAC route 2 (绘制).
Phase 1 does not render. Lint, validate, render, sandbox install, and import are 方案接线中.
Do not invoke a shell or install dependencies. Do not expose a shell to the frontend.
HTML and CSS addressable animation is the composition model. A future worker may require Node.js 22 or newer; this package does not start that worker.
Keep composition source until a DRAW01 worker exists. An MP4 is not a reversible composition source.
Handoffs, typed by the draw page and not executed here:
- import_into_editing: after render, register the product as a project resource and place it on the editing timeline.
- composite_preview: an unrendered composition may preview together with edit tracks. No MP4 is required.
- merge_render: multiple draw slots and edit tracks render together at the end. Do not force a per-slot export first.
video-use may delegate here. This package does not replace that route-2 entry.
