# vac-draw-pipeline

Executable drawing pipeline alongside video editing. Select HyperFrames HTML/GSAP or Remotion React from the composition model; set strategy=draw for source-free generation.
Read vac/tool-contracts.md before invoking tools. draw.list/save/check/render/get_frame/import share project isolation, CAS, content versions and idempotency with Editing.
A composition stores stable id, name, engine, source, even width/height, rational fps and duration_frames. Use the actual response's revision/content_version; do not substitute a stale version or a same-name object.
A full round trip is draw.save -> draw.check -> draw.render -> draw.get_frame -> artifacts.get -> draw.import -> timeline.read/edit -> run.finish. draw.import is restricted to the run's launch sequence; no human undo/redo or approvals are Agent tools.
import_into_editing is executable. composite_preview and merge_render currently use verified rendered resources through the existing multitrack preview/export path. Source-level unrendered mixing is not implemented and must not be presented as completed.
The worker uses fixed local dependencies and an installed browser, browser network isolation, and the existing FFmpeg verifier. It does not accept shell commands or silently install dependencies/download browsers/models. Missing capability returns an actionable failure.
Generated frames can be inspected through native artifact images. Machine verification and human acceptance stay separate.


Imported project images are first-class drawing assets. Read image_resources from
skill_runs.get (builtin model input includes the same catalog), or resources.list,
then add composition.images entries using stable image resource_id values.
Each instance has its own id,x,y,width,height,dx,dy,from_frame,to_frame,fit,
opacity and mode. Go supplies resource_sha256; never submit a filesystem path.
mode="layer" automatically places the image above the scene, with linear dx/dy
movement over the frame interval. mode="source" supplies the same image and
parameters for custom source placement, without an automatic duplicate overlay.
HyperFrames source uses <img src="vac-image:INSTANCE_ID"> and window.vacImages;
Remotion's default component receives {images}, with images[INSTANCE_ID].src.
Missing/removed/changed images fail explicitly; relink the resource and save the
composition to refresh its dependency fingerprint. Preserve all instance IDs.
