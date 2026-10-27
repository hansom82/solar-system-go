package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"unsafe"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// Moon is a satellite orbiting a planet.
type Moon struct {
	name     string
	radius   float32
	distance float32 // orbital radius around the parent
	speed    float32 // angular speed (rad/s)
	angle    float32
	tex      string // embedded texture key (empty = procedural)
	model    rl.Model

	realRadiusKm float64 // real equatorial radius (km) — for the true-scale mode
	radiusStyl   float32 // stylized visual radius, kept for toggling back
}

// Planet describes one body in the simulation.
type Planet struct {
	name     string
	color    rl.Color
	radius   float32 // visual radius (world units)
	a        float32 // semi-major axis of the orbit
	e        float32 // orbital eccentricity
	peri     float32 // argument of periapsis (orbit orientation in XZ)
	n        float32 // mean motion (rad/s) — derived from the real orbital period
	rotation float32 // self-spin speed (rad/s) — derived from the real day length
	tilt     float32 // axial tilt (radians) — equals the real obliquity
	M        float32 // current mean anomaly
	spin     float32 // current spin angle
	ring     bool    // draw a ring (Saturn)

	kind kind        // procedural texture style
	base color.RGBA  // base color for the texture
	tex  string      // embedded texture key (empty = procedural fallback)

	// Real physical reference values (NASA Planetary Fact Sheet), used by
	// verifyPhysics() to check the simulation against reality.
	aAU          float64 // real semi-major axis (AU)
	periodDays   float64 // real orbital period (Earth days)
	rotHours     float64 // real sidereal rotation period (hours; negative = retrograde)
	obliquity    float64 // real axial tilt / obliquity to orbit (degrees)
	realRadiusKm float64 // real equatorial radius (km) — for the true-scale mode

	radiusStyl float32 // stylized visual radius (as authored), kept for toggling

	model rl.Model // textured sphere (radius 1), lit by the Sun shader
	moons []Moon
}

// lighting holds the custom point-light shader and its uniform locations.
type lighting struct {
	shader   rl.Shader
	locLight int32
	locColor int32
	locAmb   int32
	locCam   int32
	locOcc   int32
	locNum   int32
}

// Star is a static background point, drawn as a camera-facing textured quad.
type Star struct {
	pos  rl.Vector3
	size float32 // quad half-extent in world units
	col  rl.Color
}

// Asteroid is one particle of the main belt, orbiting the Sun on a circular
// path with a small vertical scatter.
type Asteroid struct {
	r      float32 // orbital radius
	base   float32 // base angle (radians)
	speed  float32 // angular speed (rad/s)
	y      float32 // vertical offset (belt thickness)
	size   float32 // visual size
	bright uint8   // grayscale brightness
}

// kind selects a procedural texture generator.
type kind int

const (
	kindRocky kind = iota // cratered / mottled
	kindEarth             // oceans + continents + ice caps
	kindBands             // horizontal gas-giant bands
	kindCloudy            // soft swirled clouds
	kindSun               // turbulent emissive plasma
)

func main() {
	const (
		width  = 1280
		height = 720
		fov    = 60.0
	)

	rl.InitWindow(int32(width), int32(height), "Solar System — raylib-go (OpenGL)")
	defer rl.CloseWindow()

	rl.SetTargetFPS(60)

	// Camera with orbital (arcball) control.
	cam := rl.Camera3D{
		Position:   rl.Vector3{X: 0, Y: 45, Z: -90},
		Target:     rl.Vector3{},
		Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
		Fovy:       fov,
		Projection: rl.CameraPerspective,
	}

	// Planets with Keplerian elements. The visual semi-major axis (a), sizes
	// and orbit orientations (peri) are stylised for display, but the real
	// physical reference values (aAU, periodDays, rotHours, obliquity — NASA
	// Planetary Fact Sheet) drive the actual motion via calibrate(), so every
	// orbital-period and rotation-period RATIO matches reality. verifyPhysics()
	// prints a comparison table to stdout at startup.
	planets := []Planet{
		{name: "Mercury", color: rl.Color{R: 169, G: 169, B: 169, A: 255}, radius: 0.9, a: 14, e: 0.21, peri: 0.4, kind: kindRocky, base: color.RGBA{169, 169, 169, 255}, tex: "mercury",
			aAU: 0.387, periodDays: 87.97, rotHours: 1407.6, obliquity: 0.034, realRadiusKm: 2439.7},
		{name: "Venus", color: rl.Color{R: 226, G: 187, B: 137, A: 255}, radius: 1.4, a: 20, e: 0.01, peri: 1.2, kind: kindCloudy, base: color.RGBA{226, 187, 137, 255}, tex: "venus_atmosphere",
			aAU: 0.723, periodDays: 224.7, rotHours: -5832.5, obliquity: 177.4, realRadiusKm: 6051.8},
		{name: "Earth", color: rl.Color{R: 60, G: 120, B: 216, A: 255}, radius: 1.5, a: 27, e: 0.02, peri: 2.0, kind: kindEarth, base: color.RGBA{60, 120, 216, 255}, tex: "earth_daymap",
			aAU: 1.000, periodDays: 365.25, rotHours: 23.93, obliquity: 23.44, realRadiusKm: 6371.0,
			moons: []Moon{{name: "Moon", radius: 0.4, distance: 3.0, speed: 3.0, tex: "moon", realRadiusKm: 1737.4}}},
		{name: "Mars", color: rl.Color{R: 193, G: 92, B: 64, A: 255}, radius: 1.1, a: 34, e: 0.09, peri: 5.0, kind: kindRocky, base: color.RGBA{193, 92, 64, 255}, tex: "mars",
			aAU: 1.524, periodDays: 686.98, rotHours: 24.62, obliquity: 25.19, realRadiusKm: 3389.5,
			moons: []Moon{{name: "Phobos", radius: 0.18, distance: 2.0, speed: 5.0, realRadiusKm: 11.3}, {name: "Deimos", radius: 0.14, distance: 2.8, speed: 3.2, realRadiusKm: 6.2}}},
		{name: "Jupiter", color: rl.Color{R: 205, G: 164, B: 122, A: 255}, radius: 3.6, a: 48, e: 0.05, peri: 0.2, kind: kindBands, base: color.RGBA{205, 164, 122, 255}, tex: "jupiter",
			aAU: 5.203, periodDays: 4332.6, rotHours: 9.93, obliquity: 3.13, realRadiusKm: 69911,
			moons: []Moon{{name: "Io", radius: 0.3, distance: 5.5, speed: 3.4, realRadiusKm: 1821.6}, {name: "Europa", radius: 0.26, distance: 6.6, speed: 2.6, realRadiusKm: 1560.8}, {name: "Ganymede", radius: 0.4, distance: 7.8, speed: 1.9, realRadiusKm: 2634.1}, {name: "Callisto", radius: 0.38, distance: 9.2, speed: 1.3, realRadiusKm: 2410.3}}},
		{name: "Saturn", color: rl.Color{R: 216, G: 190, B: 145, A: 255}, radius: 3.0, a: 62, e: 0.06, peri: 1.6, ring: true, kind: kindBands, base: color.RGBA{216, 190, 145, 255}, tex: "saturn",
			aAU: 9.537, periodDays: 10759.2, rotHours: 10.66, obliquity: 26.73, realRadiusKm: 58232,
			moons: []Moon{{name: "Titan", radius: 0.35, distance: 7.5, speed: 1.8, realRadiusKm: 2574.7}}},
		{name: "Uranus", color: rl.Color{R: 150, G: 200, B: 220, A: 255}, radius: 2.2, a: 74, e: 0.05, peri: 3.0, kind: kindCloudy, base: color.RGBA{150, 200, 220, 255}, tex: "uranus",
			aAU: 19.191, periodDays: 30688.5, rotHours: -17.24, obliquity: 97.77, realRadiusKm: 25362},
		{name: "Neptune", color: rl.Color{R: 62, G: 96, B: 190, A: 255}, radius: 2.1, a: 86, e: 0.01, peri: 4.2, kind: kindCloudy, base: color.RGBA{62, 96, 190, 255}, tex: "neptune",
			aAU: 30.069, periodDays: 60182, rotHours: 16.11, obliquity: 28.32},
	}

	// Derive the simulation's orbital/spin rates and axial tilts from the real
	// reference values, then print a physics-verification table to stdout.
	calibrate(planets)
	verifyPhysics(planets)

	// Remember the stylized radii so the M key can toggle between the
	// display-friendly sizes and true relative proportions.
	for i := range planets {
		planets[i].radiusStyl = planets[i].radius
		for j := range planets[i].moons {
			planets[i].moons[j].radiusStyl = planets[i].moons[j].radius
		}
	}

	// Custom point-light shader (Sun at the origin lights the planets).
	light := newLighting()
	defer rl.UnloadShader(light.shader)

	// Sun: emissive textured sphere + additive halo billboard.
	sunModel := buildSunModel()
	haloTex := genHaloTexture()
	// Soft round sprite reused for star and asteroid billboards.
	dotTex := genDotTexture()
	defer rl.UnloadTexture(dotTex)
	// Soft gaussian puff (no solid core) used for the belt's volumetric fog.
	fogTex := genFogTexture()
	defer rl.UnloadTexture(fogTex)

	// Real TTF font for labels and HUD. Bake at a high base size so the
	// smaller HUD/label sizes are downscaled (crisp) rather than upscaled.
	font, fontLoaded := loadSystemFont(64)
	if fontLoaded {
		defer rl.UnloadFont(font)
	}

	// Build a textured sphere model (radius 1) for each planet and moon.
	for i := range planets {
		p := &planets[i]
		p.model = buildPlanetModel(p.kind, p.base, p.tex, light.shader)
		// Bake the axial tilt into the model's LOCAL transform (applied before
		// scale/spin/translation by DrawModelEx). This tilts the textured
		// sphere itself, so poles/equator follow the tilted spin axis (Uranus
		// lies on its side, Venus is flipped) while DrawModelEx keeps handling
		// the orbital placement — which the rlgl matrix stack broke earlier.
		p.model.Transform = rl.MatrixRotateZ(p.tilt)
		for j := range p.moons {
			p.moons[j].model = buildPlanetModel(kindRocky, color.RGBA{150, 150, 150, 255}, p.moons[j].tex, light.shader)
		}
	}

	// Static starfield + asteroid belt (positions precomputed once, drawn in
	// one batched draw call each — negligible GPU cost).
	stars := genStars(1200)
	asteroids := genAsteroids(1000)
	haze := genHaze(2600)
	fog := genFog(140)
	beltAngle := float32(0)

	timeScale := float32(1.0)
	paused := false
	showOrbits := true
	showLabels := true
	cinematic := false // auto cinematic orbit; C toggles it back to manual
	camTime := float32(0)

	// Focus view: -1 = free overview, 0 = Sun, 1..8 = planets.
	// Digit keys 1..9 select a body (1=Sun, 2=Mercury ... 9=Neptune), 0 clears.
	focus := -1
	prevFocusIdx := -1 // -1 = free overview (matches initial focus: no snap on frame 1)
	prevFocus := rl.Vector3{}

	// Planet proportions: false = stylized (readable), true = true relative
	// sizes from real radii. M toggles. sunR is the Sun's visual radius, which
	// depends on the mode.
	realScale := false
	sunR := float32(6.0)

	for !rl.WindowShouldClose() {
		dt := rl.GetFrameTime()

		// ---- Input ----
		if rl.IsKeyPressed(rl.KeyC) {
			cinematic = !cinematic
		}
		if rl.IsKeyPressed(rl.KeySpace) {
			paused = !paused
		}
		if rl.IsKeyPressed(rl.KeyO) {
			showOrbits = !showOrbits
		}
		if rl.IsKeyPressed(rl.KeyL) {
			showLabels = !showLabels
		}
		if rl.IsKeyPressed(rl.KeyF11) {
			rl.ToggleFullscreen()
		}
		if rl.IsKeyPressed(rl.KeyR) {
			for i := range planets {
				planets[i].M = 0
				planets[i].spin = 0
				for j := range planets[i].moons {
					planets[i].moons[j].angle = 0
				}
			}
		}
		// Digit keys 1..9 focus the camera on a body (1=Sun, 2=Mercury ...
		// 9=Neptune); 0 returns to the free overview. Focusing leaves manual
		// mouse orbiting intact — the camera just re-centres on the body and
		// then tracks it as it moves along its orbit.
		for k := int32(rl.KeyOne); k <= int32(rl.KeyNine); k++ {
			if rl.IsKeyPressed(k) {
				focus = int(k - int32(rl.KeyOne)) // 0=Sun .. 8=Neptune
				break
			}
		}
		if rl.IsKeyPressed(rl.KeyZero) {
			focus = -1
		}
		// M toggles planet proportions: stylized <-> true relative sizes.
		if rl.IsKeyPressed(rl.KeyM) {
			realScale = !realScale
			sunR = applySizeMode(planets, realScale)
		}
		if rl.IsKeyDown(rl.KeyRight) {
			timeScale *= 1.0 + 2.0*dt
		}
		if rl.IsKeyDown(rl.KeyLeft) {
			timeScale *= 1.0 - 2.0*dt
			if timeScale < 0.05 {
				timeScale = 0.05
			}
		}

		// ---- Update ----
		if !paused {
			for i := range planets {
				planets[i].M += planets[i].n * timeScale * float32(dt)
				planets[i].spin += planets[i].rotation * timeScale * float32(dt)
				for j := range planets[i].moons {
					planets[i].moons[j].angle += planets[i].moons[j].speed * timeScale * float32(dt)
				}
			}
			beltAngle += timeScale * float32(dt)
		}

		// ---- Camera ----
		if cinematic {
			// Cinematic auto-orbit around the whole system. Frozen while
			// paused, so SPACE visibly stops everything.
			if !paused {
				camTime += float32(dt)
				updateCinematicCamera(&cam, camTime)
			}
		} else {
			if focus >= 0 {
				// Focus view: the camera orbits the selected body and tracks
				// it along its orbit. Mouse orbiting still works as usual.
				var fp rl.Vector3
				dist := float32(30)
				if focus == 0 {
					fp = rl.Vector3{} // the Sun
				} else {
					p := planets[focus-1]
					fp = keplerPos(p)
					dist = p.radius * 8
					if dist < 10 {
						dist = 10
					}
				}
				if focus != prevFocusIdx {
					// Newly focused: keep the current view direction but
					// re-centre on the body at a comfortable distance.
					dir := rl.Vector3Normalize(rl.Vector3Subtract(cam.Position, cam.Target))
					cam.Target = fp
					cam.Position = rl.Vector3Add(fp, rl.Vector3Scale(dir, dist))
					prevFocusIdx = focus
				} else {
					// Already focused: slide the whole camera by the body's
					// movement since last frame, so it stays framed.
					delta := rl.Vector3Subtract(fp, prevFocus)
					cam.Target = rl.Vector3Add(cam.Target, delta)
					cam.Position = rl.Vector3Add(cam.Position, delta)
				}
				prevFocus = fp
			} else if prevFocusIdx != -1 {
				// Just returned to the free overview: re-centre on the Sun.
				dir := rl.Vector3Normalize(rl.Vector3Subtract(cam.Position, cam.Target))
				cam.Target = rl.Vector3{}
				cam.Position = rl.Vector3Scale(dir, 105)
				prevFocusIdx = -1
			}
			// Manual orbital camera. NOTE: rl.UpdateCamera(CameraOrbital) in
			// this raylib version only auto-orbits and ignores WASD/mouse, so
			// the controls are implemented here:
			//   right-drag mouse : yaw/pitch around the target
			//   W / S            : fly closer / farther (dolly along the view)
			//   A / D            : yaw left / right around the target
			//   mouse wheel      : zoom in / out
			// Plus a gentle auto-orbit at 0.1 rad/s (raylib's 0.5 slowed 5x),
			// frozen while paused. All rotation slows down proportionally to
			// the sim speed (1/timeScale, clamped) so fast-forward stays
			// watchable.
			rotScale := float32(1) / timeScale
			if rotScale > 1 {
				rotScale = 1
			}
			if rotScale < 0.05 {
				rotScale = 0.05
			}
			if !paused {
				view := rl.Vector3Subtract(cam.Position, cam.Target)
				view = rl.Vector3Transform(view, rl.MatrixRotateY(0.1*rotScale*dt))
				cam.Position = rl.Vector3Add(cam.Target, view)
			}
			// Mouse rotation slows down proportionally to the sim speed.
			sens := float32(0.003) * rotScale
			if rl.IsMouseButtonDown(rl.MouseRightButton) {
				md := rl.GetMouseDelta()
				rl.CameraYaw(&cam, -md.X*sens, 1)
				rl.CameraPitch(&cam, -md.Y*sens, 1, 1, 0)
			}
			// WASD: W/S dolly along the view line, A/D yaw around the target.
			view := rl.Vector3Subtract(cam.Position, cam.Target)
			d := rl.Vector3Length(view)
			if d > 0.001 {
				fwd := rl.Vector3Scale(view, 1/d)
				move := 20.0 * dt
				if rl.IsKeyDown(rl.KeyW) && d > 2 {
					cam.Position = rl.Vector3Subtract(cam.Position, rl.Vector3Scale(fwd, move))
				}
				if rl.IsKeyDown(rl.KeyS) {
					cam.Position = rl.Vector3Add(cam.Position, rl.Vector3Scale(fwd, move))
				}
			}
			if rl.IsKeyDown(rl.KeyA) {
				rl.CameraYaw(&cam, 0.8*rotScale*dt, 1)
			}
			if rl.IsKeyDown(rl.KeyD) {
				rl.CameraYaw(&cam, -0.8*rotScale*dt, 1)
			}
			// Mouse wheel zoom (clamped so the camera can't clip or fly off).
			if w := rl.GetMouseWheelMove(); w != 0 {
				view := rl.Vector3Subtract(cam.Position, cam.Target)
				nd := rl.Vector3Length(view) * float32(math.Pow(0.9, float64(w)))
				if nd < 2 {
					nd = 2
				}
				if nd > 400 {
					nd = 400
				}
				cam.Position = rl.Vector3Add(cam.Target, rl.Vector3Scale(rl.Vector3Normalize(view), nd))
			}
		}

		// ---- Draw ----
		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		rl.BeginMode3D(cam)

		// Stars glow: additive blending, no depth writes (they're a backdrop).
		rl.DisableDepthMask()
		rl.BeginBlendMode(rl.BlendAdditive)
		drawStarfield(stars, dotTex, cam)
		rl.EndBlendMode()
		rl.EnableDepthMask()

		// Elliptical orbit paths (Sun at one focus).
		if showOrbits {
			for _, p := range planets {
				drawEllipse(p)
			}
		}

		// Belt fog: large, heavily overlapping gaussian puffs that read as a
		// soft volumetric haze filling the belt's volume. Drawn first (deepest)
		// and additively with no depth writes so it glows behind everything.
		rl.DisableDepthMask()
		rl.BeginBlendMode(rl.BlendAdditive)
		drawFog(fog, beltAngle, fogTex, cam)
		// Belt haze: a soft additive band of fine dust that fakes the
		// unresolved swarm of tiny asteroids seen from afar.
		drawHaze(haze, beltAngle, dotTex, cam)
		rl.EndBlendMode()
		rl.EnableDepthMask()

		// Main asteroid belt (billboard specks, default shader).
		drawAsteroids(asteroids, beltAngle, dotTex, cam)

		// Sun: emissive textured sphere + additive halo billboard.
		rl.DrawModel(sunModel, rl.Vector3{}, sunR, rl.White)
		// The halo is a glow, not a solid object: disable depth writes so it
		// never occludes planets that pass behind the Sun.
		rl.DisableDepthMask()
		rl.BeginBlendMode(rl.BlendAdditive)
		rl.DrawBillboard(cam, haloTex, rl.Vector3{}, sunR*26.0/6.0, rl.Color{R: 255, G: 180, B: 90, A: 255})
		rl.EndBlendMode()
		rl.EnableDepthMask()

		// Planets (lit by the Sun via the custom shader).
		rl.BeginShaderMode(light.shader)
		rl.SetShaderValue(light.shader, light.locLight, []float32{0, 0, 0}, rl.ShaderUniformVec3)
		rl.SetShaderValue(light.shader, light.locColor, []float32{1.0, 0.95, 0.85}, rl.ShaderUniformVec3)
		rl.SetShaderValue(light.shader, light.locAmb, []float32{0.05}, rl.ShaderUniformFloat)
		rl.SetShaderValue(light.shader, light.locCam, []float32{cam.Position.X, cam.Position.Y, cam.Position.Z}, rl.ShaderUniformVec3)

		// Pass 1: compute every body's world position and radius and pack them
		// as occluders (vec4 = xyz center, w radius) so the shader can cast
		// real inter-body shadows (eclipses). Pass 2 draws with those shadows.
		type body struct {
			pos    rl.Vector3
			radius float32
		}
		bodies := make([]body, 0, 24)
		for _, p := range planets {
			pos := keplerPos(p)
			bodies = append(bodies, body{pos, p.radius})
			for _, m := range p.moons {
				mp := rl.Vector3{
					X: pos.X + float32(math.Cos(float64(m.angle)))*m.distance,
					Y: pos.Y,
					Z: pos.Z + float32(math.Sin(float64(m.angle)))*m.distance,
				}
				bodies = append(bodies, body{mp, m.radius})
			}
		}
		// Upload the occluder array (vec4 per body) and its count.
		occ := make([]float32, 0, len(bodies)*4)
		for _, b := range bodies {
			occ = append(occ, b.pos.X, b.pos.Y, b.pos.Z, b.radius)
		}
		if len(occ) > 0 {
			rl.SetShaderValueV(light.shader, light.locOcc, occ, rl.ShaderUniformVec4, int32(len(bodies)))
		}
		rl.SetShaderValue(light.shader, light.locNum, []float32{float32(len(bodies))}, rl.ShaderUniformFloat)

		// Pass 2: draw each body.
		for _, p := range planets {
			pos := keplerPos(p)

			// The axial tilt is baked into p.model.Transform (Rz(tilt), set once
			// at build time). After the equirect mesh fix the texture's north
			// pole is local +Y, and raymath's Rz(tilt) maps +Y to
			// (-sin t, cos t, 0) — so the spin axis must be exactly that pole,
			// otherwise the texture would wobble around a mismatched axis.
			// (Saturn's ring plane is already perpendicular to this same pole.)
			axis := rl.Vector3{
				X: -float32(math.Sin(float64(p.tilt))),
				Y: float32(math.Cos(float64(p.tilt))),
				Z: 0,
			}
			scale := rl.Vector3{X: p.radius, Y: p.radius, Z: p.radius}
			rl.DrawModelEx(p.model, pos, axis, p.spin*180.0/float32(math.Pi), scale, rl.White)

			if p.ring {
				drawRing(pos, p.radius*1.5, p.radius*2.4, p.tilt, p.color)
			}

			// Moons.
			for _, m := range p.moons {
				mp := rl.Vector3{
					X: pos.X + float32(math.Cos(float64(m.angle)))*m.distance,
					Y: pos.Y,
					Z: pos.Z + float32(math.Sin(float64(m.angle)))*m.distance,
				}
				rl.DrawModel(m.model, mp, m.radius, rl.White)
			}
		}
		rl.EndShaderMode()

		rl.EndMode3D()

		// Labels. GetWorldToScreen returns 2D screen coordinates, so the text
		// must be drawn in 2D mode (after EndMode3D) — drawing it inside the 3D
		// mode would apply the projection a second time and push it off-screen.
		if showLabels {
			for _, p := range planets {
				sp := rl.GetWorldToScreen(keplerPos(p), cam)
				if sp.X > -200 && sp.X < float32(rl.GetScreenWidth())+200 &&
					sp.Y > -50 && sp.Y < float32(rl.GetScreenHeight())+50 {
					drawText(font, fontLoaded, p.name, sp.X+6, sp.Y-6, 18, rl.White)
				}
			}
		}

		focusName := ""
		if focus == 0 {
			focusName = "Sun"
		} else if focus > 0 {
			focusName = planets[focus-1].name
		}
		drawHUD(font, fontLoaded, timeScale, paused, showOrbits, showLabels, cinematic, focusName, realScale)

		rl.EndDrawing()
	}
}

// loadSystemFont tries a handful of common TTF locations and returns the first
// font that loads, plus whether one was found.
func loadSystemFont(size int32) (rl.Font, bool) {
	paths := []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/System/Library/Fonts/Helvetica.ttc",
		"C:/Windows/Fonts/arial.ttf",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			// Bake glyphs at a high base size so DrawTextEx only ever
			// downscales them (upscaling a small atlas = blurry text).
			f := rl.LoadFontEx(p, size, nil)
			if f.BaseSize > 0 {
				// Generate a mip chain so downscaling the baked glyphs
				// stays smooth (no shimmer/aliasing at small sizes).
				rl.GenTextureMipmaps(&f.Texture)
				rl.SetTextureFilter(f.Texture, rl.FilterTrilinear)
				return f, true
			}
		}
	}
	return rl.Font{}, false
}

// keplerPos solves Kepler's equation and returns the planet's XZ position
// with the Sun at one focus of the ellipse.
func keplerPos(p Planet) rl.Vector3 {
	// Solve E - e*sin(E) = M by Newton iteration.
	E := float64(p.M)
	for i := 0; i < 6; i++ {
		E = E - (E-float64(p.e)*math.Sin(E)-float64(p.M))/(1-float64(p.e)*math.Cos(E))
	}
	// True anomaly and radius.
	xp := float64(p.a) * (math.Cos(E) - float64(p.e))
	yp := float64(p.a) * math.Sqrt(1-float64(p.e)*float64(p.e)) * math.Sin(E)
	// Rotate by argument of periapsis into the XZ plane.
	cw, sw := math.Cos(float64(p.peri)), math.Sin(float64(p.peri))
	return rl.Vector3{
		X: float32(xp*cw - yp*sw),
		Y: 0,
		Z: float32(xp*sw + yp*cw),
	}
}

// Time scales mapping real days to simulation seconds. Two separate scales
// are used because a real planet spins ~365x faster than it orbits: a single
// scale that makes orbits watchable would turn spins into an invisible blur,
// and vice versa. Each scale is internally consistent, so every orbital-period
// RATIO and every rotation-period RATIO in the simulation matches reality.
const (
	orbitDaysPerSimSec = 18.0 // Earth year ~ 20 sim-seconds
	spinDaysPerSimSec  = 0.5  // Earth day  ~ 2 sim-seconds
)

// sunRadiusKm is the Sun's equatorial radius, used for the true-proportion mode.
const sunRadiusKm = 696340.0

// applySizeMode switches the bodies between the stylized radii (authored for
// readability) and TRUE relative proportions taken from real radii in km.
//
// The true-proportion mode is normalised so Jupiter keeps its current visual
// size; every other body is scaled by realKm/JupiterKm, so the real size
// ratios (Jupiter is 11.2x Earth, Mercury 0.38x Earth, Phobos a speck) become
// visible without any planet overlapping its own orbit. The Sun is scaled by
// the same factor but clamped: at true proportion it would be ~10x Jupiter and
// would swallow the inner orbits, because the orbit distances themselves are
// stylized, not to scale.
//
// Returns the Sun's visual radius for the current mode.
func applySizeMode(planets []Planet, real bool) float32 {
	const jupiterKm = 69911.0
	const jupiterUnits = float32(3.6) // Jupiter's stylized radius = the anchor
	if !real {
		for i := range planets {
			planets[i].radius = planets[i].radiusStyl
			for j := range planets[i].moons {
				planets[i].moons[j].radius = planets[i].moons[j].radiusStyl
			}
		}
		return 6.0
	}
	k := jupiterUnits / float32(jupiterKm) // world units per km
	for i := range planets {
		p := &planets[i]
		r := float32(p.realRadiusKm) * k
		if r < 0.02 {
			r = 0.02 // keep tiny bodies (Phobos/Deimos) from vanishing entirely
		}
		p.radius = r
		for j := range p.moons {
			mr := float32(p.moons[j].realRadiusKm) * k
			if mr < 0.012 {
				mr = 0.012
			}
			p.moons[j].radius = mr
		}
	}
	sun := float32(sunRadiusKm) * k
	if sun > 5.0 { // clamp so it never engulfs Mercury's orbit (a=14)
		sun = 5.0
	}
	return sun
}

// calibrate derives each planet's mean motion (n), spin rate (rotation) and
// axial tilt from its real reference values, so the simulation reproduces the
// true period ratios and obliquities of the Solar System.
func calibrate(planets []Planet) {
	for i := range planets {
		p := &planets[i]
		// Mean motion from the real orbital period (rad per sim-second).
		p.n = float32(2 * math.Pi * orbitDaysPerSimSec / p.periodDays)
		// Spin rate magnitude from the real sidereal day. The DIRECTION of
		// rotation is NOT encoded here: it comes from the obliquity. A body
		// with obliquity > 90 deg (Venus 177, Uranus 98) has its spin axis
		// flipped past vertical, which already makes it appear to rotate
		// retrograde when spun prograde about that tilted axis. Applying the
		// negative sign here too would double-count the retrograde.
		rotDays := math.Abs(p.rotHours) / 24.0
		p.rotation = float32(2 * math.Pi * spinDaysPerSimSec / rotDays)
		// Axial tilt equals the real obliquity (full, may exceed 90 deg).
		p.tilt = float32(p.obliquity * math.Pi / 180.0)
	}
}

// verifyPhysics prints a startup table comparing the simulation's implied
// orbital period, rotation period and axial tilt against the real NASA
// reference values, and checks Kepler's third law on the real dataset
// ((P/1yr)^2 / a^3 must be ~1.000 for a genuine Solar-System orbit).
func verifyPhysics(planets []Planet) {
	fmt.Println()
	fmt.Println("=== Physics verification (NASA Planetary Fact Sheet) ===")
	fmt.Println("Kepler III: (P/1yr)^2 / a^3 must be ~1.000 for a real orbit.")
	fmt.Printf("%-8s %7s %10s %9s %9s %7s %10s %11s %9s\n",
		"Planet", "a(AU)", "P_real(d)", "P_sim(s)", "P_sim(d)", "T2/a3", "rot(h)", "rot_sim(h)", "tilt(deg)")
	for _, p := range planets {
		t2a3 := math.Pow(p.periodDays/365.25, 2) / math.Pow(p.aAU, 3)
		// Convert the simulation's rates back to real units: sim-seconds x
		// (days per sim-second) recovers the real period, proving the
		// round-trip is faithful.
		pSimSec := 2 * math.Pi / float64(p.n)
		pSimDays := pSimSec * orbitDaysPerSimSec
		rotSimSec := 2 * math.Pi / math.Abs(float64(p.rotation))
		rotSimH := math.Copysign(rotSimSec*spinDaysPerSimSec*24.0, p.rotHours)
		tiltSim := float64(p.tilt) * 180 / math.Pi
		fmt.Printf("%-8s %7.3f %10.2f %9.2f %9.2f %7.3f %10.1f %11.1f %9.2f\n",
			p.name, p.aAU, p.periodDays, pSimSec, pSimDays, t2a3, p.rotHours, rotSimH, tiltSim)
	}
	fmt.Println()
}

// drawEllipse traces the planet's elliptical orbit path.
func drawEllipse(p Planet) {
	const segs = 128
	prev := keplerPos(Planet{a: p.a, e: p.e, peri: p.peri, M: 0})
	for i := 1; i <= segs; i++ {
		M := float32(i) / segs * 2 * float32(math.Pi)
		cur := keplerPos(Planet{a: p.a, e: p.e, peri: p.peri, M: M})
		rl.DrawLine3D(prev, cur, rl.Color{R: 70, G: 70, B: 90, A: 120})
		prev = cur
	}
}

// ringBrightness returns the opacity of Saturn's rings at a normalised radius
// t in [0,1] (inner edge -> outer edge), modelling the real ring structure:
// faint C ring, bright B ring, the dark Cassini Division, the medium A ring
// and the thin Encke gap. Returns 0..1.
func ringBrightness(t float32) float32 {
	switch {
	case t < 0.14: // C ring (faint, translucent)
		return 0.30
	case t < 0.46: // B ring (brightest, densest)
		return 0.92
	case t < 0.55: // Cassini Division (the prominent dark gap)
		return 0.06
	case t < 0.90: // A ring (medium brightness)
		return 0.58
	case t < 0.93: // Encke gap (thin dark division inside the A ring)
		return 0.08
	default: // outer A / F ring fringe
		return 0.34
	}
}

// drawRing draws Saturn's rings as many dense concentric circles whose colour
// and opacity follow ringBrightness, producing the banded structure (C/B/A
// rings, Cassini Division, Encke gap) instead of a flat disc. The ring plane
// is tilted by tilt around the Z axis.
func drawRing(pos rl.Vector3, inner, outer, tilt float32, col rl.Color) {
	const segs = 128   // segments per circle (smooth)
	const rings = 150  // concentric circles (dense -> reads as a solid band)
	st, ct := float32(math.Sin(float64(tilt))), float32(math.Cos(float64(tilt)))
	for ri := 0; ri < rings; ri++ {
		t := float32(ri) / float32(rings-1)
		r := inner + (outer-inner)*t
		b := ringBrightness(t)
		if b <= 0.02 {
			continue // skip near-empty divisions
		}
		// Subtle radial mottling so the bands aren't perfectly uniform.
		b *= float32(0.85 + 0.15*math.Sin(float64(t)*90.0))
		// Ring particles are pale tan/cream, slightly warmer than the planet.
		cr := uint8(float32(col.R) * 1.05)
		cg := uint8(float32(col.G) * 1.0)
		cb := uint8(float32(col.B) * 0.88)
		alpha := uint8(b * 235)
		if cr > 250 {
			cr = 250
		}
		var prev rl.Vector3
		for i := 0; i <= segs; i++ {
			a := float32(i) / segs * 2 * float32(math.Pi)
			lx, ly := float32(math.Cos(float64(a)))*r, float32(math.Sin(float64(a)))*r
			// Ring lies in the planet's local XZ plane (perpendicular to the
			// spin axis = local Y). Apply the same Rz(tilt) as the sphere so
			// the ring stays aligned with the planet's equator:
			//   x' = lx*ct, y' = lx*st, z' = ly.
			p := rl.Vector3{X: pos.X + lx*ct, Y: pos.Y + lx*st, Z: pos.Z + ly}
			if i > 0 {
				rl.DrawLine3D(prev, p, rl.Color{R: cr, G: cg, B: cb, A: alpha})
			}
			prev = p
		}
	}
}

// loadBodyTexture returns a GPU texture for a body: the embedded real photo
// when texKey names a bundled JPEG (decoded from the binary via
// LoadImageFromMemory), otherwise a procedurally generated texture. Real
// photos get mipmaps + trilinear filtering so they stay crisp when mapped onto
// the small spheres (no shimmer at grazing angles).
func loadBodyTexture(texKey string, k kind, base color.RGBA) rl.Texture2D {
	if data := embeddedTexture(texKey); data != nil {
		img := rl.LoadImageFromMemory(".jpg", data, int32(len(data)))
		if img != nil && img.Width > 0 {
			tex := rl.LoadTextureFromImage(img)
			rl.UnloadImage(img)
			rl.GenTextureMipmaps(&tex)
			rl.SetTextureFilter(tex, rl.FilterTrilinear)
			rl.SetTextureWrap(tex, rl.WrapClamp)
			return tex
		}
	}
	// Fallback: procedural texture (no embedded photo for this body).
	img := genTexture(k, base)
	tex := rl.LoadTextureFromImage(img)
	rl.UnloadImage(img)
	rl.SetTextureFilter(tex, rl.FilterBilinear)
	return tex
}

// fixSphereMesh corrects raylib's GenMeshSphere so it matches a standard
// equirectangular planet map. Two defects (verified against par_shapes.h and a
// probe): the poles sit on the Z axis (our physics — Rz tilt, spin about
// {sin,cos,0}, ring plane — assume the pole is +Y), and the UVs are swapped
// (texU=polar/latitude, texV=azimuth/longitude) relative to equirect. We rotate
// vertices+normals by Rx(-90) to lift the north pole to +Y, and swap the UVs so
// texU=longitude, texV=latitude (v=0 at the north pole = top of the map). The
// mesh is already on the GPU (GenMeshSphere uploads it), so we push the edited
// buffers back with UpdateMeshBuffer (attr index: 0=pos, 1=uv, 2=normal).
func fixSphereMesh(mesh *rl.Mesh) {
	n := int(mesh.VertexCount)
	if n == 0 || mesh.Vertices == nil || mesh.Texcoords == nil {
		return
	}
	verts := unsafe.Slice(mesh.Vertices, n*3)
	uvs := unsafe.Slice(mesh.Texcoords, n*2)
	var norms []float32
	if mesh.Normals != nil {
		norms = unsafe.Slice(mesh.Normals, n*3)
	}
	for i := 0; i < n; i++ {
		// Rx(-90): (x,y,z) -> (x, z, -y). Maps +Z (north pole) to +Y.
		x, y, z := verts[i*3], verts[i*3+1], verts[i*3+2]
		verts[i*3], verts[i*3+1], verts[i*3+2] = x, z, -y
		if norms != nil {
			nx, ny, nz := norms[i*3], norms[i*3+1], norms[i*3+2]
			norms[i*3], norms[i*3+1], norms[i*3+2] = nx, nz, -ny
		}
		// Re-map UV for equirect WITHOUT mirroring (det=+1): the original
		// sphere has texU=polar, texV=azimuth; we want texU=longitude,
		// texV=latitude (v=0 at north). Because Rx(-90) makes increasing
		// azimuth run westward, longitude = 1-azimuth. A plain swap (v,u)
		// would mirror the map (det=-1); (1-v, u) is a rotation.
		u, v := uvs[i*2], uvs[i*2+1]
		uvs[i*2], uvs[i*2+1] = 1-v, u
	}
	f2b := func(p *float32, count int) []byte {
		return unsafe.Slice((*byte)(unsafe.Pointer(p)), count*4)
	}
	rl.UpdateMeshBuffer(*mesh, 0, f2b(mesh.Vertices, n*3), 0)
	rl.UpdateMeshBuffer(*mesh, 1, f2b(mesh.Texcoords, n*2), 0)
	if mesh.Normals != nil {
		rl.UpdateMeshBuffer(*mesh, 2, f2b(mesh.Normals, n*3), 0)
	}
}

// buildPlanetModel creates a unit-radius sphere model with a real (embedded)
// or procedural texture and the custom lighting shader.
func buildPlanetModel(k kind, base color.RGBA, texKey string, shader rl.Shader) rl.Model {
	// GenMeshSphere already uploads the mesh to the GPU in this fork.
	mesh := rl.GenMeshSphere(1, 48, 48)
	fixSphereMesh(&mesh)
	model := rl.LoadModelFromMesh(mesh)

	tex := loadBodyTexture(texKey, k, base)

	mats := model.GetMaterials()
	mats[0].Shader = shader
	rl.SetMaterialTexture(&mats[0], rl.MapAlbedo, tex)
	return model
}

// buildSunModel creates an emissive (unlit) textured sphere for the Sun.
func buildSunModel() rl.Model {
	// GenMeshSphere already uploads the mesh to the GPU in this fork.
	mesh := rl.GenMeshSphere(1, 48, 48)
	fixSphereMesh(&mesh)
	model := rl.LoadModelFromMesh(mesh)

	tex := loadBodyTexture("sun", kindSun, color.RGBA{255, 190, 70, 255})

	mats := model.GetMaterials()
	rl.SetMaterialTexture(&mats[0], rl.MapAlbedo, tex)
	return model
}

// genHaloTexture builds a radial glow sprite (white center -> transparent edge).
func genHaloTexture() rl.Texture2D {
	const s = 256
	im := image.NewRGBA(image.Rect(0, 0, s, s))
	c := float64(s) / 2
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			dx := (float64(x) - c) / c
			dy := (float64(y) - c) / c
			d := math.Sqrt(dx*dx + dy*dy)
			a := math.Max(0, 1-d)
			a = math.Pow(a, 2.2) * 255
			im.Set(x, y, color.RGBA{255, 200, 120, uint8(a)})
		}
	}
	tex := rl.LoadTextureFromImage(rl.NewImageFromImage(im))
	rl.SetTextureFilter(tex, rl.FilterBilinear)
	return tex
}

// genDotTexture builds a soft white radial dot (opaque center -> transparent
// edge). Because it is radially symmetric it reads as a round dot on a
// camera-facing quad from any angle. Used for stars and asteroid specks.
func genDotTexture() rl.Texture2D {
	const s = 64
	im := image.NewRGBA(image.Rect(0, 0, s, s))
	c := float64(s) / 2
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			dx := (float64(x) - c) / c
			dy := (float64(y) - c) / c
			d := math.Sqrt(dx*dx + dy*dy)
			// Solid bright core (so the centre stays opaque even when the
			// quad shrinks to a few pixels) with a soft glow falloff to the
			// edge. Radially symmetric -> reads round from any angle.
			var a float64
			switch {
			case d < 0.35:
				a = 1
			case d < 1:
				t := (d - 0.35) / 0.65
				a = 1 - t*t*(3-2*t)
			default:
				a = 0
			}
			im.Set(x, y, color.RGBA{255, 255, 255, uint8(a * 255)})
		}
	}
	tex := rl.LoadTextureFromImage(rl.NewImageFromImage(im))
	rl.SetTextureFilter(tex, rl.FilterBilinear)
	return tex
}

// genFogTexture builds a soft gaussian puff (no solid core, smooth falloff to
// fully transparent at the rim). Overlapping many of these reads as volumetric
// fog rather than a hard-edged sprite.
func genFogTexture() rl.Texture2D {
	const s = 128
	im := image.NewRGBA(image.Rect(0, 0, s, s))
	c := float64(s) / 2
	sigma := 0.42
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			dx := (float64(x) - c) / c
			dy := (float64(y) - c) / c
			d2 := dx*dx + dy*dy
			// Gaussian bell, clamped to 0 past the rim for a clean edge.
			a := math.Exp(-d2 / (2 * sigma * sigma))
			if d2 > 1 {
				a *= math.Max(0, 1-(math.Sqrt(d2)-1)/0.3)
			}
			if a < 0 {
				a = 0
			}
			im.Set(x, y, color.RGBA{255, 255, 255, uint8(a * 255)})
		}
	}
	tex := rl.LoadTextureFromImage(rl.NewImageFromImage(im))
	rl.SetTextureFilter(tex, rl.FilterBilinear)
	return tex
}

// genFog builds large, slowly-drifting soft puffs spread through the belt's
// volume. They are far bigger than the dust specks and overlap heavily, so the
// additive blend accumulates into a continuous foggy band. Reuses Asteroid.
func genFog(n int) []Asteroid {
	rnd := rand.New(rand.NewSource(7777))
	fs := make([]Asteroid, n)
	for i := range fs {
		r := float32(37.5 + rnd.Float64()*3.5)
		fs[i] = Asteroid{
			r:      r,
			base:   float32(rnd.Float64() * 2 * math.Pi),
			speed:  beltMeanMotion(r),
			y:      float32((rnd.Float64()*2 - 1) * 1.8),
			size:   float32(1.6 + rnd.Float64()*2.4),
			bright: uint8(40 + rnd.Intn(35)),
		}
	}
	return fs
}

// drawFog renders the belt's volumetric fog as large additive gaussian puffs.
// Caller sets additive blending + no depth writes. Low alpha keeps each puff
// faint so only the dense overlap accumulates into visible fog.
func drawFog(fs []Asteroid, beltAngle float32, fog rl.Texture2D, cam rl.Camera3D) {
	for _, a := range fs {
		ang := a.base + beltAngle*a.speed
		c, s := float32(math.Cos(float64(ang))), float32(math.Sin(float64(ang)))
		pos := rl.Vector3{X: c * a.r, Y: a.y, Z: s * a.r}
		col := rl.Color{R: a.bright, G: a.bright, B: a.bright, A: 46}
		rl.DrawBillboard(cam, fog, pos, a.size, col)
	}
}

// matrix uniforms matModel/matView/matProjection and binds the albedo texture
// to the sampler named "texture0". Attributes use the fork's fixed locations
// (0=vertexPosition, 1=vertexTexCoord, 2=vertexNormal).
const lightingVS = `#version 330
uniform mat4 matModel;
uniform mat4 matView;
uniform mat4 matProjection;
layout(location=0) in vec3 vertexPosition;
layout(location=1) in vec2 vertexTexCoord;
layout(location=2) in vec3 vertexNormal;
out vec2 fragTexcoords;
out vec3 fragWorldPos;
out vec3 fragNormal;
void main() {
    fragTexcoords = vertexTexCoord;
    fragWorldPos = (matModel * vec4(vertexPosition, 1.0)).xyz;
    fragNormal = normalize(mat3(matModel) * vertexNormal);
    gl_Position = matProjection * matView * matModel * vec4(vertexPosition, 1.0);
}`

const lightingFS = `#version 330
in vec2 fragTexcoords;
in vec3 fragWorldPos;
in vec3 fragNormal;
out vec4 fragColor;
uniform sampler2D texture0;
uniform vec3 lightPos;
uniform vec3 lightColor;
uniform float ambient;
uniform vec3 camPos;
uniform vec4 occluders[32];
uniform float numOccluders;

// shadowFactor returns 1 (fully lit) .. 0 (fully eclipsed). The point light is
// at lightPos; every lit body is a sphere (occluders[i] = xyz center, w radius).
// We cast a ray from the fragment toward the light and test each sphere for
// overlap, producing real cast shadows (e.g. a moon falling into a planet's
// shadow). A body never shadows itself: for a lit fragment its own center lies
// behind the ray, so t <= 0 and the sphere is skipped.
float shadowFactor(vec3 fp, vec3 L, float distL) {
    float sh = 1.0;
    for (int i = 0; i < int(numOccluders); i++) {
        vec3 c = occluders[i].xyz;
        float r = occluders[i].w;
        vec3 oc = c - fp;
        float t = dot(oc, L);
        if (t <= 0.0 || t >= distL) continue;   // sphere not between fragment and light
        float d = sqrt(max(dot(oc, oc) - t * t, 0.0)); // perpendicular distance to ray
        float s = smoothstep(r, r * 0.55, d);   // soft penumbra near the rim
        sh = min(sh, 1.0 - s);
    }
    return sh;
}

void main() {
    vec3 albedoCol = texture(texture0, fragTexcoords).rgb;
    vec3 N = normalize(fragNormal);
    vec3 Lv = lightPos - fragWorldPos;
    float dist = length(Lv);
    vec3 L = Lv / dist;
    float diff = max(dot(N, L), 0.0);
    // gentle distance falloff so far planets are still lit
    float atten = 1.0 / (1.0 + 0.0009 * dist * dist);
    float sh = shadowFactor(fragWorldPos, L, dist);
    vec3 V = normalize(camPos - fragWorldPos);
    vec3 H = normalize(L + V);
    float spec = pow(max(dot(N, H), 0.0), 24.0) * diff * sh;
    // High contrast: low ambient, and the shadow multiplies the diffuse term so
    // night sides and eclipse shadows fall close to black.
    vec3 lit = albedoCol * (ambient + diff * atten * sh) + lightColor * spec * 0.3;
    fragColor = vec4(lit, 1.0);
}`

// newLighting compiles the lighting shader and caches uniform locations.
func newLighting() lighting {
	sh := rl.LoadShaderFromMemory(lightingVS, lightingFS)
	return lighting{
		shader:   sh,
		locLight: rl.GetShaderLocation(sh, "lightPos"),
		locColor: rl.GetShaderLocation(sh, "lightColor"),
		locAmb:   rl.GetShaderLocation(sh, "ambient"),
		locCam:   rl.GetShaderLocation(sh, "camPos"),
		locOcc:   rl.GetShaderLocation(sh, "occluders"),
		locNum:   rl.GetShaderLocation(sh, "numOccluders"),
	}
}

// genTexture builds a procedural equirectangular texture for a planet.
func genTexture(k kind, base color.RGBA) *rl.Image {
	const w, h = 256, 128
	px := make([]color.RGBA, w*h)
	for y := 0; y < h; y++ {
		v := float64(y) / h
		for x := 0; x < w; x++ {
			u := float64(x) / w
			c := base
			switch k {
			case kindRocky:
				n := fbm(u*8, v*8, 4)
				shade := 0.6 + 0.5*n
				c = scale(base, shade)
			case kindBands:
				band := math.Sin(v*math.Pi*10 + fbm(u*4, v*6, 3)*2)
				shade := 0.75 + 0.3*band
				c = scale(base, shade)
			case kindCloudy:
				n := fbm(u*5, v*5, 5)
				c = scale(base, 0.7+0.5*n)
			case kindSun:
				n := fbm(u*6, v*6, 5)
				c = scale(base, 0.85+0.4*n)
			case kindEarth:
				land := fbm(u*4, v*4, 5)
				if v < 0.08 || v > 0.92 {
					c = color.RGBA{240, 240, 245, 255} // ice caps
				} else if land > 0.55 {
					c = color.RGBA{70, 130, 70, 255} // continents
				} else {
					c = scale(base, 0.8+0.3*land) // ocean
				}
			}
			px[y*w+x] = c
		}
	}
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(im.Pix, bytesOf(px))
	return rl.NewImageFromImage(im)
}

// bytesOf reinterprets RGBA pixels as a byte slice for image.Pix.
func bytesOf(px []color.RGBA) []byte {
	out := make([]byte, len(px)*4)
	for i, c := range px {
		out[i*4] = c.R
		out[i*4+1] = c.G
		out[i*4+2] = c.B
		out[i*4+3] = c.A
	}
	return out
}

func scale(c color.RGBA, f float64) color.RGBA {
	cl := func(v uint8) uint8 {
		x := float64(v) * f
		if x > 255 {
			x = 255
		}
		return uint8(x)
	}
	return color.RGBA{cl(c.R), cl(c.G), cl(c.B), 255}
}

// hash + value noise + fbm for procedural textures.
func hash(x, y float64) float64 {
	s := math.Sin(x*127.1+y*311.7) * 43758.5453
	return s - math.Floor(s)
}

func vnoise(x, y float64) float64 {
	xi, yi := math.Floor(x), math.Floor(y)
	xf, yf := x-xi, y-yi
	u := xf * xf * (3 - 2*xf)
	v := yf * yf * (3 - 2*yf)
	a := hash(xi, yi)
	b := hash(xi+1, yi)
	c := hash(xi, yi+1)
	d := hash(xi+1, yi+1)
	return a*(1-u)*(1-v) + b*u*(1-v) + c*(1-u)*v + d*u*v
}

func fbm(x, y float64, oct int) float64 {
	sum, amp, freq := 0.0, 0.5, 1.0
	for i := 0; i < oct; i++ {
		sum += amp * vnoise(x*freq, y*freq)
		freq *= 2
		amp *= 0.5
	}
	return sum
}

// drawStarfield draws a static cloud of points around the scene.
// genStars builds a static starfield on a large sphere shell. Each star is a
// camera-facing textured quad (a soft round dot), so it looks correct from any
// viewing angle.
func genStars(n int) []Star {
	rnd := rand.New(rand.NewSource(1337))
	stars := make([]Star, n)
	for i := range stars {
		// Uniform direction on the unit sphere.
		u := rnd.Float64()*2 - 1
		phi := rnd.Float64() * 2 * math.Pi
		s := math.Sqrt(1 - u*u)
		dx, dy, dz := s*math.Cos(phi), u, s*math.Sin(phi)

		// Radius shell so stars sit far behind the planets.
		r := 380 + rnd.Float64()*120
		pos := rl.Vector3{X: float32(dx * r), Y: float32(dy * r), Z: float32(dz * r)}

		// Tiny dust-mote specks; a few slightly brighter "beacon" stars.
		size := float32(0.3 + rnd.Float64()*0.4)
		if rnd.Float64() < 0.06 {
			size *= 1.8
		}

		// Slight colour variation: white-blue to warm.
		warm := rnd.Float64()
		cr := uint8(255)
		cg := uint8(235 + rnd.Intn(20))
		cb := uint8(210 + rnd.Intn(45))
		if warm > 0.8 {
			cr, cg, cb = 255, 220, 180
		}
		alpha := uint8(170 + rnd.Intn(85))
		stars[i] = Star{pos: pos, size: size, col: rl.Color{R: cr, G: cg, B: cb, A: alpha}}
	}
	return stars
}

// beltMeanMotion returns the calibrated mean motion (rad/sim-sec) for a body
// at display orbit radius r, derived from Kepler's third law so it is
// consistent with calibrate(). The display radius is mapped to a real
// semi-major axis by linear interpolation between the Mars (a=34 -> 1.524 AU)
// and Jupiter (a=48 -> 5.203 AU) anchors, then P = a^1.5 years and
// n = 2*pi*orbitDaysPerSimSec / P. This keeps the belt orbiting slower than
// Mars and faster than Jupiter, with inner rocks faster than outer ones.
func beltMeanMotion(r float32) float32 {
	const (
		marsA, marsAU     = 34.0, 1.524
		jupA, jupAU       = 48.0, 5.203
		daysPerYear       = 365.25
	)
	au := marsAU + (float64(r)-marsA)/(jupA-marsA)*(jupAU-marsAU)
	periodDays := math.Pow(au, 1.5) * daysPerYear
	return float32(2 * math.Pi * orbitDaysPerSimSec / periodDays)
}

// genAsteroids builds the main asteroid belt between Mars and Jupiter.
func genAsteroids(n int) []Asteroid {
	rnd := rand.New(rand.NewSource(9001))
	as := make([]Asteroid, n)
	for i := range as {
		// Real main belt sits at 2.1-3.3 AU. Mapped through the Mars(34 ->
		// 1.524 AU)/Jupiter(48 -> 5.203 AU) anchors that is ~36.2-40.8 scene
		// units, but Mars's aphelion here is 34*(1+0.09)=37.06, so the inner
		// edge is nudged to 37.5 to keep the belt clear of Mars's orbit.
		// Result: ~2.4-3.4 AU, with a gap to Jupiter's perihelion (45.6).
		r := float32(37.5 + rnd.Float64()*3.5)
		// Keplerian speed consistent with the calibrated planets.
		speed := beltMeanMotion(r)
		as[i] = Asteroid{
			r:      r,
			base:   float32(rnd.Float64() * 2 * math.Pi),
			speed:  speed,
			y:      float32((rnd.Float64()*2 - 1) * 1.6),
			// Kept small so no speck ever rivals a planet: largest ~0.22 vs
			// Mercury's 0.9 radius. A few slightly bigger "lead" rocks.
			size:   float32(0.06 + rnd.Float64()*0.16),
			bright: uint8(120 + rnd.Intn(90)),
		}
	}
	return as
}

// genHaze builds a dense field of tiny dust specks filling the belt, drawn
// additively as a soft glow to fake the unresolved swarm of small asteroids
// that reads as a faint band from a distance. Same radial band as the rocks.
func genHaze(n int) []Asteroid {
	rnd := rand.New(rand.NewSource(4242))
	hs := make([]Asteroid, n)
	for i := range hs {
		r := float32(37.5 + rnd.Float64()*3.5)
		hs[i] = Asteroid{
			r:      r,
			base:   float32(rnd.Float64() * 2 * math.Pi),
			speed:  beltMeanMotion(r),
			y:      float32((rnd.Float64()*2 - 1) * 1.4),
			size:   float32(0.10 + rnd.Float64()*0.22),
			bright: uint8(70 + rnd.Intn(60)),
		}
	}
	return hs
}

// drawStarfield renders every star as a camera-facing billboard sprite using
// rl.DrawBillboard (the same proven path as the Sun halo). Additive blending
// (set by the caller) makes them glow. One DrawBillboard per star.
func drawStarfield(stars []Star, dot rl.Texture2D, cam rl.Camera3D) {
	for _, s := range stars {
		rl.DrawBillboard(cam, dot, s.pos, s.size, s.col)
	}
}

// drawAsteroids renders the belt as camera-facing billboard specks using
// rl.DrawBillboard. Normal alpha blending (caller) keeps them looking like
// solid rocky dust rather than glowing points.
func drawAsteroids(as []Asteroid, beltAngle float32, dot rl.Texture2D, cam rl.Camera3D) {
	for _, a := range as {
		ang := a.base + beltAngle*a.speed
		c, s := float32(math.Cos(float64(ang))), float32(math.Sin(float64(ang)))
		pos := rl.Vector3{X: c * a.r, Y: a.y, Z: s * a.r}
		col := rl.Color{R: a.bright, G: a.bright, B: a.bright, A: 230}
		rl.DrawBillboard(cam, dot, pos, a.size, col)
	}
}

// drawHaze renders the belt's fine dust as soft, faint additive billboards.
// Caller sets additive blending; the low alpha and small size make the swarm
// read as a continuous faint band rather than individual points.
func drawHaze(hs []Asteroid, beltAngle float32, dot rl.Texture2D, cam rl.Camera3D) {
	for _, a := range hs {
		ang := a.base + beltAngle*a.speed
		c, s := float32(math.Cos(float64(ang))), float32(math.Sin(float64(ang)))
		pos := rl.Vector3{X: c * a.r, Y: a.y, Z: s * a.r}
		col := rl.Color{R: a.bright, G: a.bright, B: a.bright, A: 70}
		rl.DrawBillboard(cam, dot, pos, a.size, col)
	}
}

func drawText(font rl.Font, loaded bool, s string, x, y, size float32, col rl.Color) {
	if loaded {
		rl.DrawTextEx(font, s, rl.Vector2{X: x, Y: y}, size, 1, col)
	} else {
		rl.DrawText(s, int32(x), int32(y), int32(size), col)
	}
}

func drawHUD(font rl.Font, loaded bool, timeScale float32, paused, orbits, labels, cinematic bool, focusName string, realScale bool) {
	drawText(font, loaded, "Solar System - raylib-go", 12, 10, 26, rl.White)
	drawText(font, loaded, "C: cinematic   RMB-drag: orbit   W/S: zoom   A/D: yaw   SPACE: pause   <-/->: speed   O: orbits   L: labels   R: reset   F11: fullscreen", 12, 44, 16, rl.LightGray)
	drawText(font, loaded, "1: Sun  2-9: Mercury..Neptune  0: overview   M: sizes", 12, 66, 16, rl.LightGray)
	status := "RUNNING"
	if paused {
		status = "PAUSED"
	}
	if cinematic {
		status += "  [CINEMATIC]"
	}
	if realScale {
		status += "  [TRUE SIZES]"
	} else {
		status += "  [STYLED SIZES]"
	}
	if focusName != "" {
		status += "  FOCUS: " + focusName
	}
	drawText(font, loaded, status+"  x"+fmt.Sprintf("%.2f", timeScale), 12, float32(rl.GetScreenHeight())-34, 20, rl.Yellow)
	_ = orbits
	_ = labels
}

// updateCinematicCamera drives a smooth auto-orbit around the Sun. The azimuth
// sweeps continuously while elevation and distance oscillate on slow, offset
// sine curves, producing a crane-up/dolly-in feel that reveals the system (and
// its starfield) from many different angles. All motion is continuous in time,
// so there are no jumps when the mode is toggled on.
func updateCinematicCamera(cam *rl.Camera3D, t float32) {
	const rad = float64(math.Pi) / 180.0
	// 5x slower than the original sweep (was ~11 deg/s azimuth).
	az := float64(t) * 2.2 * rad               // ~2.2 deg/s azimuth sweep
	el := (18 + 26*math.Sin(float64(t)*0.046)) * rad // height swings -8..44 deg (5x slower)
	dist := 105 + 45*math.Sin(float64(t)*0.034)      // dolly 60..150 units (5x slower)
	ce := math.Cos(el)
	cam.Position = rl.Vector3{
		X: float32(dist * ce * math.Sin(az)),
		Y: float32(dist * math.Sin(el)),
		Z: float32(dist * ce * math.Cos(az)),
	}
	cam.Target = rl.Vector3{}
	cam.Up = rl.Vector3{X: 0, Y: 1, Z: 0}
}
