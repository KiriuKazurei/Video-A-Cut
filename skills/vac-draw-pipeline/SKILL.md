# vac-draw-pipeline

Instruction-only shared draw pipeline for VAC route 2 (绘制).
It describes how vac-draw-hyperframes and vac-draw-remotion meet the editing timeline.
Phase 1 does not call a worker. Lint, render, sandbox, and the three handoffs are 方案接线中.
Do not invoke a shell or install dependencies. Do not expose a shell to the frontend.
import_into_editing registers a finished render as a project resource and an editing clip.
composite_preview composites unrendered slots with edit tracks and does not require an MP4.
merge_render combines every selected draw slot with the edit tracks in one final render.
Choose the engine from the composition model. HyperFrames is the default page structure; Remotion is for a React component tree. Mixing a draw layer with an edit track does not create a third product route.
