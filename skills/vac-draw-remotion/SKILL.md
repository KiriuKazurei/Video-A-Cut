# vac-draw-remotion

Executable Remotion drawing Skill for VAC. Use strategy=draw; an empty current project is valid.
Read vac/tool-contracts.md. Author one TSX module exporting a default React component. Imports are limited to react and remotion; use AbsoluteFill, Sequence, useCurrentFrame, useVideoConfig, interpolate and spring for frame-addressed motion.
Use inline graphics and bound current-project image resources. Add external audio/video through the editing timeline. No remote imports, Node APIs, arbitrary paths, shell or installation commands.
draw.save stores source/specification with the current project session/revision and an idempotency key; draw.check checks syntax/imports/specification; draw.render uses the installed official renderer and Chromium without downloading a browser or using a cloud fallback.
Use draw.get_frame plus artifacts.get to inspect real pixels, then draw.import into the run's launch sequence. timeline.read/edit can change the resulting clips; run.finish takes the current sequence_revision and content_version.
Source-level composite_preview and merge_render use the shared FrameSource after draw.import source_mode=snapshot (or explicit follow), without an intermediate MP4. Omitted source_mode retains verified rendered import.
Retain editable source in the AVP. Do not invent human acceptance; machine completion reports not_checked.


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
