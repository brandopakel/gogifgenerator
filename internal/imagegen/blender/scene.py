# SPDX-License-Identifier: GPL-3.0-or-later
# This small Blender-side program is intentionally separate from the Go binary.

import argparse
import colorsys
import json
import math
import random
import sys

import bpy


def arguments():
    values = sys.argv[sys.argv.index("--") + 1 :]
    parser = argparse.ArgumentParser()
    parser.add_argument("--request", required=True)
    parser.add_argument("--output", required=True)
    return parser.parse_args(values)


def material(name, hue, metallic=0.25, roughness=0.3):
    red, green, blue = colorsys.hsv_to_rgb(hue % 1.0, 0.72, 0.95)
    value = bpy.data.materials.new(name)
    value.diffuse_color = (red, green, blue, 1.0)
    value.metallic = metallic
    value.roughness = roughness
    return value


def look_at_origin(obj):
    direction = -obj.location
    obj.rotation_euler = direction.to_track_quat("-Z", "Y").to_euler()


def build_scene(settings, output):
    bpy.ops.wm.read_factory_settings(use_empty=True)
    seed = int(settings.get("seed", 1)) & 0x7FFFFFFF
    random.seed(seed)
    scene = bpy.context.scene
    scene.render.engine = "BLENDER_EEVEE"
    scene.render.resolution_x = int(settings["width"])
    scene.render.resolution_y = int(settings["height"])
    scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = "PNG"
    scene.render.image_settings.color_mode = "RGBA"
    scene.render.filepath = output
    scene.render.film_transparent = False
    scene.world = bpy.data.worlds.new("GoGIF World")
    scene.world.color = (0.008, 0.01, 0.018)

    bpy.ops.object.camera_add(location=(0.0, -0.25, 12.5))
    camera = bpy.context.object
    camera.data.type = "ORTHO"
    camera.data.ortho_scale = 10.5
    look_at_origin(camera)
    scene.camera = camera

    for location, energy, size, hue in [
        ((4.5, -3.0, 7.0), 900, 5.0, 0.16),
        ((-4.0, 2.5, 5.5), 750, 4.0, 0.58),
        ((0.0, 4.0, 4.0), 550, 3.0, 0.92),
    ]:
        bpy.ops.object.light_add(type="AREA", location=location)
        light = bpy.context.object
        light.data.energy = energy
        light.data.shape = "DISK"
        light.data.size = size
        light.data.color = colorsys.hsv_to_rgb(hue, 0.55, 1.0)
        look_at_origin(light)

    bpy.ops.mesh.primitive_plane_add(size=24, location=(0, 0, -1.55))
    floor = bpy.context.object
    floor.data.materials.append(material("floor", 0.63, 0.05, 0.52))

    base_hue = random.random()
    for index in range(7):
        angle = (index / 7.0) * math.tau + random.uniform(-0.18, 0.18)
        radius = random.uniform(2.0, 4.0)
        location = (math.cos(angle) * radius, math.sin(angle) * radius, random.uniform(-0.4, 1.25))
        shape = index % 3
        if shape == 0:
            bpy.ops.mesh.primitive_ico_sphere_add(subdivisions=2, radius=random.uniform(0.45, 1.0), location=location)
        elif shape == 1:
            bpy.ops.mesh.primitive_torus_add(major_radius=random.uniform(0.5, 0.9), minor_radius=random.uniform(0.12, 0.25), location=location)
        else:
            bpy.ops.mesh.primitive_cube_add(size=random.uniform(0.65, 1.35), location=location)
            bpy.context.object.rotation_euler = tuple(random.uniform(-0.7, 0.7) for _ in range(3))
        obj = bpy.context.object
        obj.data.materials.append(material("shape-%d" % index, base_hue + index * 0.115, 0.35, 0.24))

    prompt = " ".join(str(settings.get("prompt", "MAKE IT MOVE")).upper().split())[:28]
    bpy.ops.object.text_add(location=(0, -0.2, 1.0))
    text = bpy.context.object
    text.data.body = prompt
    text.data.align_x = "CENTER"
    text.data.align_y = "CENTER"
    text.data.extrude = 0.11
    text.data.bevel_depth = 0.025
    text.data.size = min(1.15, max(0.48, 15.0 / max(8, len(prompt))))
    text.data.materials.append(material("type", base_hue + 0.48, 0.05, 0.22))

    bpy.ops.render.render(write_still=True)


if __name__ == "__main__":
    args = arguments()
    with open(args.request, "r", encoding="utf-8") as handle:
        request = json.load(handle)
    build_scene(request, args.output)
