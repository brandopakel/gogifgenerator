package intent

// The lexicons below are the offline reading of a prompt. They are
// deliberately small, explicit, and data-only so the interpretation is
// reviewable, deterministic, and testable without a model or a network call.

var styleDescriptors = map[string]string{
	StylePhotoreal:    "photorealistic, natural materials, real-world lighting",
	StyleIllustration: "clean illustration, confident linework, flat shapes",
	StyleAnime:        "anime key art, cel shading, expressive features",
	StyleRetro:        "retro analog film, grain, period-accurate detail",
	StylePainterly:    "painterly brushwork, visible strokes, rich pigment",
	StyleRender3D:     "stylized 3D render, soft global illumination",
	StylePixel:        "pixel art, limited palette, crisp pixel edges",
}

var moodDescriptors = map[string]string{
	MoodCalm:      "calm, unhurried, soft even light",
	MoodEnergetic: "energetic, kinetic, high contrast",
	MoodDramatic:  "dramatic, moody, strong directional light",
	MoodJoyful:    "joyful, bright, saturated and warm",
	MoodEerie:     "eerie, uneasy, cold desaturated light",
	MoodTender:    "tender, intimate, gentle warm light",
}

var cameraDescriptors = map[string]string{
	CameraStatic:   "locked-off camera, stable framing",
	CameraOrbit:    "camera arcing around the subject",
	CameraPushIn:   "camera pushing in toward the subject",
	CameraPullBack: "camera pulling back to reveal the scene",
	CameraPan:      "camera panning across the scene",
	CameraHandheld: "handheld camera, slight natural sway",
}

// universalNegative names the artifacts that ruin a GIF source frame at every
// style. Style-specific negatives are appended by negativeFor.
const universalNegative = "collage, split screen, multiple panels, storyboard, border, frame, caption, text, watermark, logo, signature, blurry, low quality, distorted anatomy, extra limbs"

var styleNegatives = map[string]string{
	StylePhotoreal:    "cartoon, anime, illustration, 3d render, plastic skin",
	StyleIllustration: "photograph, photorealistic, harsh noise",
	StyleAnime:        "photograph, photorealistic, 3d render",
	StyleRetro:        "modern clothing, modern signage, clean digital sharpness",
	StylePainterly:    "photograph, vector art, flat digital gradient",
	StyleRender3D:     "photograph, hand-drawn linework",
	StylePixel:        "photograph, smooth gradients, antialiased edges",
}

func negativeFor(style string) string {
	if extra := styleNegatives[style]; extra != "" {
		return universalNegative + ", " + extra
	}
	return universalNegative
}

// styleMarkers map a user's words onto the closed style vocabulary. Longer
// phrases are matched before single words by the interpreter.
var styleMarkers = map[string]string{
	"photorealistic": StylePhotoreal,
	"photoreal":      StylePhotoreal,
	"photo":          StylePhotoreal,
	"photograph":     StylePhotoreal,
	"realistic":      StylePhotoreal,
	"cinematic":      StylePhotoreal,
	"film":           StylePhotoreal,
	"illustration":   StyleIllustration,
	"illustrated":    StyleIllustration,
	"cartoon":        StyleIllustration,
	"comic":          StyleIllustration,
	"sketch":         StyleIllustration,
	"doodle":         StyleIllustration,
	"anime":          StyleAnime,
	"manga":          StyleAnime,
	"ghibli":         StyleAnime,
	"retro":          StyleRetro,
	"vintage":        StyleRetro,
	"vhs":            StyleRetro,
	"analog":         StyleRetro,
	"nostalgic":      StyleRetro,
	"1980s":          StyleRetro,
	"80s":            StyleRetro,
	"90s":            StyleRetro,
	"painterly":      StylePainterly,
	"painting":       StylePainterly,
	"painted":        StylePainterly,
	"watercolor":     StylePainterly,
	"oil":            StylePainterly,
	"impressionist":  StylePainterly,
	"3d":             StyleRender3D,
	"render":         StyleRender3D,
	"cgi":            StyleRender3D,
	"claymation":     StyleRender3D,
	"clay":           StyleRender3D,
	"pixar":          StyleRender3D,
	"pixel":          StylePixel,
	"pixelated":      StylePixel,
	"8-bit":          StylePixel,
	"16-bit":         StylePixel,
	"voxel":          StylePixel,
}

var moodMarkers = map[string]string{
	"calm":        MoodCalm,
	"serene":      MoodCalm,
	"peaceful":    MoodCalm,
	"quiet":       MoodCalm,
	"still":       MoodCalm,
	"meditative":  MoodCalm,
	"slow":        MoodCalm,
	"energetic":   MoodEnergetic,
	"fast":        MoodEnergetic,
	"frantic":     MoodEnergetic,
	"chaotic":     MoodEnergetic,
	"wild":        MoodEnergetic,
	"explosive":   MoodEnergetic,
	"intense":     MoodEnergetic,
	"dramatic":    MoodDramatic,
	"epic":        MoodDramatic,
	"moody":       MoodDramatic,
	"dark":        MoodDramatic,
	"serious":     MoodDramatic,
	"noir":        MoodDramatic,
	"stormy":      MoodDramatic,
	"happy":       MoodJoyful,
	"joyful":      MoodJoyful,
	"cheerful":    MoodJoyful,
	"funny":       MoodJoyful,
	"silly":       MoodJoyful,
	"party":       MoodJoyful,
	"celebrating": MoodJoyful,
	"celebration": MoodJoyful,
	"birthday":    MoodJoyful,
	"festive":     MoodJoyful,
	"creepy":      MoodEerie,
	"eerie":       MoodEerie,
	"spooky":      MoodEerie,
	"haunted":     MoodEerie,
	"ominous":     MoodEerie,
	"uncanny":     MoodEerie,
	"cozy":        MoodTender,
	"tender":      MoodTender,
	"gentle":      MoodTender,
	"romantic":    MoodTender,
	"warm":        MoodTender,
	"sweet":       MoodTender,
	"wholesome":   MoodTender,
}

var cameraMarkers = map[string]string{
	"orbit":     CameraOrbit,
	"orbiting":  CameraOrbit,
	"spinning":  CameraOrbit,
	"spin":      CameraOrbit,
	"rotating":  CameraOrbit,
	"circling":  CameraOrbit,
	"turntable": CameraOrbit,
	"zoom":      CameraPushIn,
	"zooming":   CameraPushIn,
	"closeup":   CameraPushIn,
	"close-up":  CameraPushIn,
	"macro":     CameraPushIn,
	"pushing":   CameraPushIn,
	"wide":      CameraPullBack,
	"aerial":    CameraPullBack,
	"panoramic": CameraPullBack,
	"establish": CameraPullBack,
	"reveal":    CameraPullBack,
	"pan":       CameraPan,
	"panning":   CameraPan,
	"tracking":  CameraPan,
	"sweeping":  CameraPan,
	"dolly":     CameraPan,
	"handheld":  CameraHandheld,
	"shaky":     CameraHandheld,
	"vlog":      CameraHandheld,
	"found":     CameraHandheld,
}

// cameraPhrases are two-word forms whose meaning differs from either word
// alone. They are matched before the single-word table.
var cameraPhrases = map[string]string{
	"zoom in":      CameraPushIn,
	"push in":      CameraPushIn,
	"zoom out":     CameraPullBack,
	"pull back":    CameraPullBack,
	"pulling back": CameraPullBack,
	"wide shot":    CameraPullBack,
}

// actionVerbs catches common bare-infinitive verbs. Gerunds are detected by
// suffix, so this list only needs the forms an "-ing" test would miss.
var actionVerbs = map[string]bool{
	"runs": true, "run": true, "jump": true, "jumps": true, "dance": true, "dances": true,
	"fly": true, "flies": true, "fall": true, "falls": true, "explode": true, "explodes": true,
	"dive": true, "dives": true, "swim": true, "swims": true, "climb": true, "climbs": true,
	"ride": true, "rides": true, "drive": true, "drives": true, "walk": true, "walks": true,
	"eat": true, "eats": true, "drink": true, "drinks": true, "throw": true, "throws": true,
	"catch": true, "catches": true, "wave": true, "waves": true, "laugh": true, "laughs": true,
	"cry": true, "cries": true, "sleep": true, "sleeps": true, "fight": true, "fights": true,
	"play": true, "plays": true, "sing": true, "sings": true, "read": true, "reads": true,
	"build": true, "builds": true, "paint": true, "paints": true, "cook": true, "cooks": true,
	"launch": true, "launches": true, "land": true, "lands": true, "crash": true, "crashes": true,
	"chase": true, "chases": true, "escape": true, "escapes": true, "float": true, "floats": true,
	"glow": true, "glows": true, "burn": true, "burns": true, "melt": true, "melts": true,
	"smile": true, "smiles": true, "stare": true, "stares": true, "shrug": true, "shrugs": true,
	"nod": true, "nods": true, "point": true, "points": true, "clap": true, "claps": true,
	"typing": true, "types": true, "sits": true, "sit": true, "stands": true, "stand": true,
}

// gerundExceptions are "-ing" words that are ordinary nouns or adjectives in
// this domain, so treating them as the action would misread the subject.
var gerundExceptions = map[string]bool{
	"thing": true, "king": true, "ring": true, "string": true, "wing": true, "spring": true,
	"morning": true, "evening": true, "building": true, "lightning": true, "ceiling": true,
	"everything": true, "something": true, "nothing": true, "during": true, "viking": true,
	"painting": true, "drawing": true, "clothing": true,
}

var prepositions = map[string]bool{
	"in": true, "inside": true, "on": true, "onto": true, "at": true, "under": true,
	"underneath": true, "over": true, "above": true, "below": true, "near": true,
	"beside": true, "between": true, "through": true, "across": true, "beneath": true,
	"atop": true, "around": true, "behind": true, "against": true, "toward": true,
	"towards": true, "into": true, "outside": true, "amid": true, "along": true,
}

var articles = map[string]bool{"a": true, "an": true, "the": true, "some": true, "this": true, "that": true}

var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "and": true, "or": true, "but": true,
	"to": true, "for": true, "with": true, "without": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "being": true, "been": true, "it": true,
	"its": true, "as": true, "by": true, "from": true, "then": true, "than": true,
	"very": true, "really": true, "make": true, "makes": true, "making": true,
	"create": true, "creating": true, "generate": true, "please": true, "gif": true,
	"animation": true, "animated": true, "show": true, "showing": true, "me": true,
	"my": true, "i": true, "want": true, "like": true, "looks": true, "look": true,
	"kind": true, "sort": true, "some": true, "this": true, "that": true, "there": true,
	"their": true, "them": true, "they": true, "he": true, "she": true, "his": true,
	"her": true, "you": true, "your": true, "we": true, "our": true, "at": true,
	"in": true, "on": true, "into": true, "onto": true, "up": true, "down": true,
	"out": true, "off": true, "so": true, "just": true, "can": true, "will": true,
}
