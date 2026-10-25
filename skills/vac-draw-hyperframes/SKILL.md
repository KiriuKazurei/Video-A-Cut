# vac-draw-hyperframes

Executable HyperFrames drawing Skill for VAC. Use strategy=draw; the current project may have no source media.
Read vac/tool-contracts.md before calling tools. Use only the declared draw/timeline/artifact tools; no shell, filesystem scans, dependency installation or remote assets.
Author one complete HTML document with a data-composition-id root and matching data-width, data-height and data-duration (seconds). Use inline CSS/SVG and paused GSAP timelines registered in window.__timelines[rootId]. GSAP is supplied locally; do not add a script src.
draw.save commits source and dimensions to the AVP with project session, project revision and an idempotency key.
Then draw.check, draw.render, draw.get_frame and artifacts.get inspect actual rendered frames. Runtime dependencies and an installed Chromium must exist; missing dependencies fail explicitly without downloads or cloud fallback.
draw.import adds the verified video to the run's launch sequence as a separate Agent track group. Use timeline.read/edit to trim, split, apply effects or combine with source media. Finish using run.finish {sequence_revision,content_version} after the final timeline edit.
import_into_editing is executable. composite_preview and merge_render currently use rendered resources in Editing; unrendered source-level composition is not implemented. Never claim otherwise.
Human acceptance is not_checked, not automatic approval. Preserve source for subsequent code edits; MP4 is not reversible source.


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
