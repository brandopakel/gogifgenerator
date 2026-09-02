"""GoGIF Unreal Engine 5 unattended beauty-frame renderer.

The Go process supplies only a private, bounded manifest path. This script
imports the Blender FBX, applies GoGIF's portable motion contract, lights the
scene, and emits one PNG per requested frame for GoGIF to validate.
"""

import json
import math
import os
import re
import time

import unreal


def command_value(name):
    match = re.search(r"(?:^|\s)-%s=(?:\"([^\"]+)\"|(\S+))" % re.escape(name), unreal.SystemLibrary.get_command_line())
    return (match.group(1) or match.group(2)) if match else ""


def validate(manifest):
    if manifest.get("version") != 1:
        raise RuntimeError("Unsupported GoGIF manifest version")
    if not (128 <= int(manifest["width"]) <= 3840 and 128 <= int(manifest["height"]) <= 3840):
        raise RuntimeError("GoGIF frame dimensions are outside safe bounds")
    if not 4 <= int(manifest["frames"]) <= 1800:
        raise RuntimeError("GoGIF frame count is outside safe bounds")
    paths = manifest["paths"]
    if not os.path.isfile(paths["blender_asset"]) or not os.path.isfile(paths["unity_motion"]):
        raise RuntimeError("Blender asset or portable motion contract is missing")


def import_asset(filename):
    task = unreal.AssetImportTask()
    task.filename = filename
    task.destination_path = "/Game/GoGIF/Generated"
    task.destination_name = "GoGIFAsset"
    task.automated = True
    task.replace_existing = True
    task.replace_existing_settings = True
    task.save = False
    task.async_ = False
    unreal.AssetToolsHelpers.get_asset_tools().import_asset_tasks([task])
    objects = list(task.get_objects())
    meshes = [value for value in objects if isinstance(value, unreal.StaticMesh)]
    if not meshes:
        raise RuntimeError("Unreal did not import a StaticMesh from Blender FBX")
    return meshes[0]


def build_reference_card(filename, actor_subsystem, width, height):
    task = unreal.AssetImportTask()
    task.filename = filename
    task.destination_path = "/Game/GoGIF/Generated"
    task.destination_name = "GoGIFReferenceTexture"
    task.automated = True
    task.replace_existing = True
    task.replace_existing_settings = True
    task.save = False
    task.async_ = False
    asset_tools = unreal.AssetToolsHelpers.get_asset_tools()
    asset_tools.import_asset_tasks([task])
    textures = [value for value in task.get_objects() if isinstance(value, unreal.Texture2D)]
    if not textures:
        raise RuntimeError("Unreal did not import the semantic reference texture")

    material = asset_tools.create_asset(
        "GoGIFReferenceMaterial",
        "/Game/GoGIF/Generated",
        unreal.Material,
        unreal.MaterialFactoryNew(),
    )
    if not material:
        material = unreal.load_asset("/Game/GoGIF/Generated/GoGIFReferenceMaterial")
    if not material:
        raise RuntimeError("Unreal did not create the semantic reference material")
    material.set_editor_property("two_sided", True)
    texture_sample = unreal.MaterialEditingLibrary.create_material_expression(
        material, unreal.MaterialExpressionTextureSample, -320, 0
    )
    texture_sample.set_editor_property("texture", textures[0])
    if not unreal.MaterialEditingLibrary.connect_material_property(
        texture_sample, "RGB", unreal.MaterialProperty.MP_EMISSIVE_COLOR
    ):
        raise RuntimeError("Unreal could not connect the semantic reference material")
    unreal.MaterialEditingLibrary.recompile_material(material)

    plane = unreal.load_asset("/Engine/BasicShapes/Plane.Plane")
    if not plane:
        raise RuntimeError("Unreal's built-in Plane mesh is unavailable")
    longest = float(max(width, height))
    card_height = 900.0 * height / longest
    card_center_z = card_height * 0.5
    card = actor_subsystem.spawn_actor_from_object(
        plane,
        unreal.Vector(0.0, 0.0, card_center_z),
        unreal.Rotator(0.0, 90.0, 0.0),
        True,
    )
    card.set_actor_scale3d(unreal.Vector(9.0 * height / longest, 9.0 * width / longest, 1.0))
    card.get_component_by_class(unreal.StaticMeshComponent).set_material(0, material)
    return card, unreal.Vector(0.0, 0.0, card_center_z)


def render(manifest):
    paths = manifest["paths"]
    with open(paths["unity_motion"], "r", encoding="utf-8") as handle:
        motion = json.load(handle)
    if motion.get("version") != 1 or len(motion.get("frames", [])) != int(manifest["frames"]):
        raise RuntimeError("GoGIF motion contract is invalid")
    os.makedirs(paths["unreal_frames"], exist_ok=True)

    actor_subsystem = unreal.get_editor_subsystem(unreal.EditorActorSubsystem)
    mesh = import_asset(paths["blender_asset"])
    subject = actor_subsystem.spawn_actor_from_object(mesh, unreal.Vector(0.0, 0.0, 0.0), unreal.Rotator(0.0, 0.0, 0.0), True)
    card, target = build_reference_card(
        paths["reference_image"], actor_subsystem, int(manifest["width"]), int(manifest["height"])
    )
    radius = 500.0
    camera_distance = radius * 2.7
    camera = actor_subsystem.spawn_actor_from_class(
        unreal.CineCameraActor,
        # Blender's -Y-facing card becomes -X-facing after Unreal's FBX axis
        # conversion. Photograph it along X rather than seeing its thin edge.
        unreal.Vector(float(target.x) - camera_distance, float(target.y), float(target.z) + radius * 0.05),
        unreal.Rotator(),
        True,
    )
    camera.set_actor_rotation(unreal.MathLibrary.find_look_at_rotation(camera.get_actor_location(), target), False)
    camera_component = camera.get_cine_camera_component()
    camera_component.set_aspect_ratio(float(manifest["width"]) / float(manifest["height"]))
    camera_component.set_constraint_aspect_ratio(True)
    filmback = unreal.CameraFilmbackSettings(
        sensor_width=36.0,
        sensor_height=36.0 * float(manifest["height"]) / float(manifest["width"]),
    )
    camera_component.set_editor_property("filmback", filmback)
    camera_component.set_editor_property("current_focal_length", 50.0)

    sun = actor_subsystem.spawn_actor_from_class(unreal.DirectionalLight, unreal.Vector(), unreal.Rotator(0.0, -38.0, -28.0), True)
    sun.get_component_by_class(unreal.DirectionalLightComponent).set_editor_property("intensity", 8.0)
    sky = actor_subsystem.spawn_actor_from_class(unreal.SkyLight, unreal.Vector(), unreal.Rotator(), True)
    sky.get_component_by_class(unreal.SkyLightComponent).set_editor_property("intensity", 1.2)

    world = unreal.get_editor_subsystem(unreal.UnrealEditorSubsystem).get_editor_world()
    unreal.AutomationLibrary.set_scalability_quality_to_epic(world)
    # Imported textures/materials compile asynchronously. Without this barrier
    # the screenshots capture Unreal's grey checkerboard fallback material.
    unreal.AutomationLibrary.finish_loading_before_screenshot()
    unreal.EditorPythonScripting.set_keep_python_script_alive(True)

    @unreal.AutomationScheduler.add_latent_command
    def capture_frames():
        try:
            # Allow completed shader maps to bind on the render thread.
            for _ in range(18):
                yield
            for index, pose in enumerate(motion["frames"]):
                yaw = float(pose.get("yaw", 0.0))
                pitch = float(pose.get("pitch", 0.0))
                subject.set_actor_rotation(unreal.Rotator(0.0, pitch, yaw), False)
                camera_x = float(pose.get("camera_x", 0.0)) * radius * 0.12
                camera_y = float(pose.get("camera_y", 0.0)) * radius * 0.12
                zoom = max(0.8, float(pose.get("camera_zoom", 1.0)))
                camera.set_actor_location(
                    unreal.Vector(
                        float(target.x) - camera_distance,
                        float(target.y) + camera_x,
                        float(target.z) + radius * 0.05 + camera_y,
                    ),
                    False,
                    False,
                )
                camera.set_actor_rotation(unreal.MathLibrary.find_look_at_rotation(camera.get_actor_location(), target), False)
                camera_component.set_editor_property("current_focal_length", 50.0 * zoom)
                output = os.path.join(paths["unreal_frames"], "frame-%04d.png" % index)
                task = unreal.AutomationLibrary.take_high_res_screenshot(
                    int(manifest["width"]), int(manifest["height"]), output, camera, False, False,
                    unreal.ComparisonTolerance.LOW, "GoGIF cinematic frame", 0.15 if index == 0 else 0.0, True
                )
                if not task.is_valid_task():
                    raise RuntimeError("Unreal did not schedule screenshot frame %d" % index)
                deadline = time.monotonic() + 60.0
                while not task.is_task_done():
                    if time.monotonic() >= deadline:
                        raise RuntimeError("Unreal screenshot frame %d timed out" % index)
                    yield
                if not os.path.isfile(output):
                    raise RuntimeError("Unreal screenshot frame %d is missing" % index)
        finally:
            actor_subsystem.destroy_actors([subject, card, camera, sun, sky])
            unreal.EditorPythonScripting.set_keep_python_script_alive(False)


manifest_path = command_value("gogifManifest")
if not manifest_path or not os.path.isfile(manifest_path):
    raise RuntimeError("-gogifManifest is required")
with open(manifest_path, "r", encoding="utf-8") as manifest_file:
    job = json.load(manifest_file)
validate(job)
render(job)
