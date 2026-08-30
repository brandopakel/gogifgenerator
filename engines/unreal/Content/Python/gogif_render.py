"""GoGIF Unreal Engine 5 unattended beauty-frame renderer.

The Go process supplies only a private, bounded manifest path. This script
imports the Blender FBX, applies Unity's portable motion contract, lights the
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
    if not (128 <= int(manifest["width"]) <= 1024 and 128 <= int(manifest["height"]) <= 1024):
        raise RuntimeError("GoGIF frame dimensions are outside safe bounds")
    if not 4 <= int(manifest["frames"]) <= 48:
        raise RuntimeError("GoGIF frame count is outside safe bounds")
    paths = manifest["paths"]
    if not os.path.isfile(paths["blender_asset"]) or not os.path.isfile(paths["unity_motion"]):
        raise RuntimeError("Blender asset or Unity motion contract is missing")


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


def wait_for_capture(task, filename):
    deadline = time.monotonic() + 60.0
    while not task.is_task_done() and time.monotonic() < deadline:
        time.sleep(0.05)
    if not task.is_task_done() or not os.path.isfile(filename):
        raise RuntimeError("Unreal screenshot task did not finish")


def render(manifest):
    paths = manifest["paths"]
    with open(paths["unity_motion"], "r", encoding="utf-8") as handle:
        motion = json.load(handle)
    if motion.get("version") != 1 or len(motion.get("frames", [])) != int(manifest["frames"]):
        raise RuntimeError("Unity motion contract is invalid")
    os.makedirs(paths["unreal_frames"], exist_ok=True)

    actor_subsystem = unreal.get_editor_subsystem(unreal.EditorActorSubsystem)
    mesh = import_asset(paths["blender_asset"])
    subject = actor_subsystem.spawn_actor_from_object(mesh, unreal.Vector(0.0, 0.0, 0.0), unreal.Rotator(0.0, 0.0, 0.0), True)
    camera = actor_subsystem.spawn_actor_from_class(unreal.CineCameraActor, unreal.Vector(850.0, -850.0, 560.0), unreal.Rotator(), True)
    target = unreal.Vector(0.0, 0.0, 80.0)
    camera.set_actor_rotation(unreal.MathLibrary.find_look_at_rotation(camera.get_actor_location(), target), False)
    camera.get_cine_camera_component().set_editor_property("current_focal_length", 48.0)

    sun = actor_subsystem.spawn_actor_from_class(unreal.DirectionalLight, unreal.Vector(), unreal.Rotator(-38.0, -28.0, 0.0), True)
    sun.get_component_by_class(unreal.DirectionalLightComponent).set_editor_property("intensity", 8.0)
    sky = actor_subsystem.spawn_actor_from_class(unreal.SkyLight, unreal.Vector(), unreal.Rotator(), True)
    sky.get_component_by_class(unreal.SkyLightComponent).set_editor_property("intensity", 1.2)

    world = unreal.EditorLevelLibrary.get_editor_world()
    unreal.AutomationLibrary.set_scalability_quality_to_epic(world)
    try:
        for index, pose in enumerate(motion["frames"]):
            yaw = float(pose.get("yaw", 0.0))
            pitch = float(pose.get("pitch", 0.0))
            subject.set_actor_rotation(unreal.Rotator(pitch, yaw, 0.0), False)
            camera_x = float(pose.get("camera_x", 0.0)) * 90.0
            camera_y = float(pose.get("camera_y", 0.0)) * 90.0
            zoom = max(0.8, float(pose.get("camera_zoom", 1.0)))
            camera.set_actor_location(unreal.Vector(850.0 + camera_x, -850.0 + camera_y, 560.0), False, False)
            camera.set_actor_rotation(unreal.MathLibrary.find_look_at_rotation(camera.get_actor_location(), target), False)
            camera.get_cine_camera_component().set_editor_property("current_focal_length", 48.0 * zoom)
            output = os.path.join(paths["unreal_frames"], "frame-%04d.png" % index)
            task = unreal.AutomationLibrary.take_high_res_screenshot(
                int(manifest["width"]), int(manifest["height"]), output, camera, False, False,
                unreal.ComparisonTolerance.LOW, "GoGIF cinematic frame", 0.0, True
            )
            wait_for_capture(task, output)
    finally:
        actor_subsystem.destroy_actors([subject, camera, sun, sky])


manifest_path = command_value("gogifManifest")
if not manifest_path or not os.path.isfile(manifest_path):
    raise RuntimeError("-gogifManifest is required")
with open(manifest_path, "r", encoding="utf-8") as manifest_file:
    job = json.load(manifest_file)
validate(job)
render(job)
