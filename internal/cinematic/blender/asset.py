# SPDX-License-Identifier: GPL-3.0-or-later
# This Blender-side program is intentionally separate from the Go binary.

import argparse
import colorsys
import json
import math
import os
import random
import sys

import bpy


def arguments():
    values = sys.argv[sys.argv.index("--") + 1 :]
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", required=True)
    return parser.parse_args(values)


def material(name, hue, metallic=0.25, roughness=0.3, saturation=0.72, brightness=0.72):
    red, green, blue = colorsys.hsv_to_rgb(hue % 1.0, saturation, brightness)
    value = bpy.data.materials.new(name)
    value.diffuse_color = (red, green, blue, 1.0)
    value.metallic = metallic
    value.roughness = roughness
    return value


def reference_material(path):
    image = bpy.data.images.load(path, check_existing=False)
    value = bpy.data.materials.new("GoGIF_Reference")
    value.use_nodes = True
    nodes = value.node_tree.nodes
    principled = nodes.get("Principled BSDF")
    texture = nodes.new("ShaderNodeTexImage")
    texture.image = image
    texture.interpolation = "Linear"
    principled.inputs["Metallic"].default_value = 0.0
    principled.inputs["Roughness"].default_value = 0.48
    value.node_tree.links.new(texture.outputs["Color"], principled.inputs["Base Color"])
    value.node_tree.links.new(texture.outputs["Alpha"], principled.inputs["Alpha"])
    return value


def look_at_origin(obj):
    direction = -obj.location
    obj.rotation_euler = direction.to_track_quat("-Z", "Y").to_euler()


def build_scene(manifest):
    bpy.ops.wm.read_factory_settings(use_empty=True)
    seed = int(manifest.get("seed", 1)) & 0x7FFFFFFF
    random.seed(seed)
    paths = manifest["paths"]
    os.makedirs(os.path.dirname(paths["blender_asset"]), exist_ok=True)

    scene = bpy.context.scene
    scene.render.engine = "BLENDER_EEVEE"
    scene.render.resolution_x = int(manifest["width"])
    scene.render.resolution_y = int(manifest["height"])
    scene.render.resolution_percentage = 100
    scene.render.image_settings.file_format = "PNG"
    scene.render.image_settings.color_mode = "RGBA"
    scene.render.filepath = paths["blender_preview"]
    scene.render.film_transparent = False
    scene.world = bpy.data.worlds.new("GoGIF World")
    scene.world.color = (0.004, 0.006, 0.012)
    scene.view_settings.look = "AgX - Medium High Contrast"

    bpy.ops.object.camera_add(location=(0.0, -0.25, 12.5))
    camera = bpy.context.object
    camera.data.type = "ORTHO"
    camera.data.ortho_scale = 10.5
    look_at_origin(camera)
    scene.camera = camera

    for location, energy, size, hue in [
        ((4.5, -3.0, 7.0), 480, 5.0, 0.16),
        ((-4.0, 2.5, 5.5), 360, 4.0, 0.58),
        ((0.0, 4.0, 4.0), 240, 3.0, 0.92),
    ]:
        bpy.ops.object.light_add(type="AREA", location=location)
        light = bpy.context.object
        light.data.energy = energy
        light.data.shape = "DISK"
        light.data.size = size
        light.data.color = colorsys.hsv_to_rgb(hue, 0.55, 1.0)
        look_at_origin(light)

    bpy.ops.mesh.primitive_plane_add(size=10.5, location=(0, 0, -1.55))
    floor = bpy.context.object
    floor.name = "GoGIF_Floor"
    floor.data.materials.append(material("floor", 0.63, 0.0, 0.62, 0.55, 0.055))

    reference_path = paths.get("reference_image", "")
    has_reference = bool(reference_path and os.path.isfile(reference_path))
    if has_reference:
        width = float(manifest["width"])
        height = float(manifest["height"])
        longest = max(width, height)
        bpy.ops.mesh.primitive_plane_add(size=2.0, location=(0.0, 0.0, -0.02))
        reference = bpy.context.object
        reference.name = "GoGIF_Semantic_Reference"
        reference.scale = (4.5 * width / longest, 4.5 * height / longest, 1.0)
        reference.data.materials.append(reference_material(reference_path))
    else:
        base_hue = random.random()
        for index in range(9):
            angle = (index / 9.0) * math.tau + random.uniform(-0.18, 0.18)
            radius = random.uniform(1.7, 4.0)
            location = (math.cos(angle) * radius, math.sin(angle) * radius, random.uniform(-0.4, 1.4))
            shape = index % 3
            if shape == 0:
                bpy.ops.mesh.primitive_ico_sphere_add(subdivisions=3, radius=random.uniform(0.45, 1.0), location=location)
            elif shape == 1:
                bpy.ops.mesh.primitive_torus_add(major_radius=random.uniform(0.5, 0.9), minor_radius=random.uniform(0.12, 0.25), location=location)
            else:
                bpy.ops.mesh.primitive_cube_add(size=random.uniform(0.65, 1.35), location=location)
                bpy.context.object.rotation_euler = tuple(random.uniform(-0.7, 0.7) for _ in range(3))
            obj = bpy.context.object
            obj.name = "GoGIF_Asset_%02d" % index
            obj.data.materials.append(material("shape-%d" % index, base_hue + index * 0.097, 0.42, 0.2, 0.78, 0.86))

    bpy.ops.render.render(write_still=True)

    bpy.ops.object.select_all(action="DESELECT")
    meshes = [obj for obj in scene.objects if obj.type == "MESH"]
    for obj in meshes:
        obj.select_set(True)
    bpy.context.view_layer.objects.active = meshes[0]
    if len(meshes) > 1:
        bpy.ops.object.join()
    bpy.context.object.name = "GoGIF_Combined_Asset"
    bpy.ops.export_scene.fbx(
        filepath=paths["blender_asset"],
        use_selection=True,
        apply_unit_scale=True,
        bake_anim=False,
        path_mode="COPY",
        embed_textures=True,
    )


if __name__ == "__main__":
    args = arguments()
    with open(args.manifest, "r", encoding="utf-8") as handle:
        build_scene(json.load(handle))
