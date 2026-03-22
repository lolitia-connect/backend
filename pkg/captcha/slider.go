package captcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	sliderBgWidth     = 560
	sliderBgHeight    = 280
	sliderBlockSize   = 100
	sliderMinX        = 140
	sliderMaxX        = 420
	sliderTolerance   = 6
	sliderExpiry      = 5 * time.Minute
	sliderTokenExpiry = 30 * time.Second
)

type sliderShape int

const (
	shapeSquare    sliderShape = 0
	shapeCircle    sliderShape = 1
	shapeDiamond   sliderShape = 2
	shapeStar      sliderShape = 3
	shapeTriangle  sliderShape = 4
	shapeTrapezoid sliderShape = 5
)

type sliderService struct {
	redis *redis.Client
}

func newSliderService(redisClient *redis.Client) *sliderService {
	return &sliderService{redis: redisClient}
}

// sliderData stores the correct position and shape in Redis
type sliderData struct {
	X     int         `json:"x"`
	Y     int         `json:"y"`
	Shape sliderShape `json:"shape"`
}

// inMask returns true if pixel (dx,dy) within the block bounding box belongs to the shape
func inMask(dx, dy int, shape sliderShape) bool {
	half := sliderBlockSize / 2
	switch shape {
	case shapeCircle:
		ex := dx - half
		ey := dy - half
		return ex*ex+ey*ey <= half*half
	case shapeDiamond:
		return abs(dx-half)+abs(dy-half) <= half
	case shapeStar:
		return inStar(dx, dy, half)
	case shapeTriangle:
		return inTriangle(dx, dy)
	case shapeTrapezoid:
		return inTrapezoid(dx, dy)
	default: // shapeSquare
		margin := 8
		return dx >= margin && dx < sliderBlockSize-margin && dy >= margin && dy < sliderBlockSize-margin
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// pointInPolygon uses ray-casting to test if (x,y) is inside the polygon defined by pts.
func pointInPolygon(x, y float64, pts [][2]float64) bool {
	n := len(pts)
	inside := false
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := pts[i][0], pts[i][1]
		xj, yj := pts[j][0], pts[j][1]
		if ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi)+xi) {
			inside = !inside
		}
		j = i
	}
	return inside
}

// inStar returns true if (dx,dy) is inside a 5-pointed star centered in the block.
func inStar(dx, dy, half int) bool {
	cx, cy := float64(half), float64(half)
	r1 := float64(half) * 0.92 // outer radius
	r2 := float64(half) * 0.40 // inner radius
	x := float64(dx) - cx
	y := float64(dy) - cy
	pts := make([][2]float64, 10)
	for i := 0; i < 10; i++ {
		angle := float64(i)*math.Pi/5 - math.Pi/2
		r := r1
		if i%2 == 1 {
			r = r2
		}
		pts[i] = [2]float64{r * math.Cos(angle), r * math.Sin(angle)}
	}
	return pointInPolygon(x, y, pts)
}

// inTriangle returns true if (dx,dy) is inside an upward-pointing triangle.
func inTriangle(dx, dy int) bool {
	margin := 5
	size := sliderBlockSize - 2*margin
	half := float64(sliderBlockSize) / 2
	ax, ay := half, float64(margin)
	bx, by := float64(margin), float64(margin+size)
	cx, cy2 := float64(margin+size), float64(margin+size)
	px, py := float64(dx), float64(dy)
	d1 := (px-bx)*(ay-by) - (ax-bx)*(py-by)
	d2 := (px-cx)*(by-cy2) - (bx-cx)*(py-cy2)
	d3 := (px-ax)*(cy2-ay) - (cx-ax)*(py-ay)
	hasNeg := (d1 < 0) || (d2 < 0) || (d3 < 0)
	hasPos := (d1 > 0) || (d2 > 0) || (d3 > 0)
	return !(hasNeg && hasPos)
}

// inTrapezoid returns true if (dx,dy) is inside a trapezoid (wider at bottom).
func inTrapezoid(dx, dy int) bool {
	margin := 5
	topY := float64(margin)
	bottomY := float64(sliderBlockSize - margin)
	totalH := bottomY - topY
	half := float64(sliderBlockSize) / 2
	topHalfW := float64(sliderBlockSize) * 0.25
	bottomHalfW := float64(sliderBlockSize) * 0.45
	x, y := float64(dx), float64(dy)
	if y < topY || y > bottomY {
		return false
	}
	t := (y - topY) / totalH
	hw := topHalfW + t*(bottomHalfW-topHalfW)
	return x >= half-hw && x <= half+hw
}

func (s *sliderService) GenerateSlider(ctx context.Context) (id string, bgImage string, blockImage string, err error) {
	bg := generateBackground()

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	x := sliderMinX + r.Intn(sliderMaxX-sliderMinX)
	y := r.Intn(sliderBgHeight - sliderBlockSize)
	shape := sliderShape(r.Intn(6))

	block := cropBlockShaped(bg, x, y, shape)
	cutBackgroundShaped(bg, x, y, shape)

	bgB64, err := imageToPNGBase64(bg)
	if err != nil {
		return "", "", "", err
	}
	blockB64, err := imageToPNGBase64(block)
	if err != nil {
		return "", "", "", err
	}

	id = uuid.New().String()
	data, _ := json.Marshal(sliderData{X: x, Y: y, Shape: shape})
	key := fmt.Sprintf("captcha:slider:%s", id)
	if err = s.redis.Set(ctx, key, string(data), sliderExpiry).Err(); err != nil {
		return "", "", "", err
	}

	return id, bgB64, blockB64, nil
}

func (s *sliderService) Generate(ctx context.Context) (id string, image string, err error) {
	id, _, _, err = s.GenerateSlider(ctx)
	return id, "", err
}

// TrailPoint records a pointer position and timestamp during drag
type TrailPoint struct {
	X int   `json:"x"`
	Y int   `json:"y"`
	T int64 `json:"t"` // milliseconds since drag start
}

// validateTrail performs human-behaviour checks on the drag trail.
//
// Rules:
//  1. Trail must be provided and have >= 8 points
//  2. Total drag duration: 300ms – 15000ms
//  3. First point x <= 10 (started from left)
//  4. No single-step jump > 80px
//  5. Final x within tolerance*2 of declared x
//  6. Speed variance > 0 (not perfectly uniform / robotic)
//  7. Y-axis total deviation >= 2px (path is not a perfect horizontal line)
func validateTrail(trail []TrailPoint, declaredX int) bool {
	if len(trail) < 8 {
		return false
	}

	duration := trail[len(trail)-1].T - trail[0].T
	if duration < 300 || duration > 15000 {
		return false
	}

	if trail[0].X > 10 {
		return false
	}

	// Collect per-step speeds and check max jump
	var speeds []float64
	for i := 1; i < len(trail); i++ {
		dt := float64(trail[i].T - trail[i-1].T)
		dx := float64(trail[i].X - trail[i-1].X)
		dy := float64(trail[i].Y - trail[i-1].Y)
		if abs(int(dx)) > 80 {
			return false
		}
		if dt > 0 {
			dist := math.Sqrt(dx*dx + dy*dy)
			speeds = append(speeds, dist/dt)
		}
	}

	// Speed variance check – robot drag tends to be perfectly uniform
	if len(speeds) >= 3 {
		mean := 0.0
		for _, v := range speeds {
			mean += v
		}
		mean /= float64(len(speeds))
		variance := 0.0
		for _, v := range speeds {
			d := v - mean
			variance += d * d
		}
		variance /= float64(len(speeds))
		// If variance is essentially 0, it's robotic
		if variance < 1e-6 {
			return false
		}
	}

	// Y-axis deviation: humans almost always move slightly on Y
	minY := trail[0].Y
	maxY := trail[0].Y
	for _, p := range trail {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	if maxY-minY < 2 {
		return false
	}

	// Final position check
	lastX := trail[len(trail)-1].X
	if diff := abs(lastX - declaredX); diff > sliderTolerance*2 {
		return false
	}

	return true
}

func (s *sliderService) VerifySlider(ctx context.Context, id string, x, y int, trail string) (token string, err error) {
	// Trail is mandatory
	if trail == "" {
		return "", fmt.Errorf("trail required")
	}
	var points []TrailPoint
	if jsonErr := json.Unmarshal([]byte(trail), &points); jsonErr != nil {
		return "", fmt.Errorf("invalid trail")
	}
	if !validateTrail(points, x) {
		return "", fmt.Errorf("trail validation failed")
	}

	key := fmt.Sprintf("captcha:slider:%s", id)
	val, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("captcha not found or expired")
	}

	var data sliderData
	if err = json.Unmarshal([]byte(val), &data); err != nil {
		return "", fmt.Errorf("invalid captcha data")
	}

	diffX := abs(x - data.X)
	diffY := abs(y - data.Y)
	if diffX > sliderTolerance || diffY > sliderTolerance {
		s.redis.Del(ctx, key)
		return "", fmt.Errorf("position mismatch")
	}

	s.redis.Del(ctx, key)

	sliderToken := uuid.New().String()
	tokenKey := fmt.Sprintf("captcha:slider:token:%s", sliderToken)
	if err = s.redis.Set(ctx, tokenKey, "1", sliderTokenExpiry).Err(); err != nil {
		return "", err
	}

	return sliderToken, nil
}

func (s *sliderService) VerifySliderToken(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	tokenKey := fmt.Sprintf("captcha:slider:token:%s", token)
	val, err := s.redis.Get(ctx, tokenKey).Result()
	if err != nil {
		return false, nil
	}
	if val != "1" {
		return false, nil
	}
	s.redis.Del(ctx, tokenKey)
	return true, nil
}

func (s *sliderService) Verify(ctx context.Context, token string, code string, ip string) (bool, error) {
	return s.VerifySliderToken(ctx, token)
}

func (s *sliderService) GetType() CaptchaType {
	return CaptchaTypeSlider
}

// cropBlockShaped copies pixels within the shape mask from bg into a new block image.
// Pixels outside the mask are transparent. A 2-pixel white border is drawn along the shape edge.
func cropBlockShaped(bg *image.NRGBA, x, y int, shape sliderShape) *image.NRGBA {
	block := image.NewNRGBA(image.Rect(0, 0, sliderBlockSize, sliderBlockSize))
	for dy := 0; dy < sliderBlockSize; dy++ {
		for dx := 0; dx < sliderBlockSize; dx++ {
			if inMask(dx, dy, shape) {
				block.SetNRGBA(dx, dy, bg.NRGBAAt(x+dx, y+dy))
			}
		}
	}

	// Draw 2-pixel bright border along shape edge
	borderColor := color.NRGBA{R: 255, G: 255, B: 255, A: 230}
	for dy := 0; dy < sliderBlockSize; dy++ {
		for dx := 0; dx < sliderBlockSize; dx++ {
			if !inMask(dx, dy, shape) {
				continue
			}
			nearEdge := false
		check:
			for ddy := -2; ddy <= 2; ddy++ {
				for ddx := -2; ddx <= 2; ddx++ {
					if abs(ddx)+abs(ddy) > 2 {
						continue
					}
					nx, ny := dx+ddx, dy+ddy
					if nx < 0 || nx >= sliderBlockSize || ny < 0 || ny >= sliderBlockSize || !inMask(nx, ny, shape) {
						nearEdge = true
						break check
					}
				}
			}
			if nearEdge {
				block.SetNRGBA(dx, dy, borderColor)
			}
		}
	}
	return block
}

// cutBackgroundShaped blanks the shape area and draws a border outline
func cutBackgroundShaped(bg *image.NRGBA, x, y int, shape sliderShape) {
	holeColor := color.NRGBA{R: 0, G: 0, B: 0, A: 100}
	borderColor := color.NRGBA{R: 255, G: 255, B: 255, A: 220}

	// Fill hole
	for dy := 0; dy < sliderBlockSize; dy++ {
		for dx := 0; dx < sliderBlockSize; dx++ {
			if inMask(dx, dy, shape) {
				bg.SetNRGBA(x+dx, y+dy, holeColor)
			}
		}
	}

	// Draw 2-pixel border along hole edge
	for dy := 0; dy < sliderBlockSize; dy++ {
		for dx := 0; dx < sliderBlockSize; dx++ {
			if !inMask(dx, dy, shape) {
				continue
			}
			nearEdge := false
		check:
			for ddy := -2; ddy <= 2; ddy++ {
				for ddx := -2; ddx <= 2; ddx++ {
					if abs(ddx)+abs(ddy) > 2 {
						continue
					}
					nx, ny := dx+ddx, dy+ddy
					if nx < 0 || nx >= sliderBlockSize || ny < 0 || ny >= sliderBlockSize || !inMask(nx, ny, shape) {
						nearEdge = true
						break check
					}
				}
			}
			if nearEdge {
				bg.SetNRGBA(x+dx, y+dy, borderColor)
			}
		}
	}
}

// generateBackground creates a colorful 320x160 background image
func generateBackground() *image.NRGBA {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	img := image.NewNRGBA(image.Rect(0, 0, sliderBgWidth, sliderBgHeight))

	blockW := 60
	blockH := 60
	palette := []color.NRGBA{
		{R: 70, G: 130, B: 180, A: 255},
		{R: 60, G: 179, B: 113, A: 255},
		{R: 205, G: 92, B: 92, A: 255},
		{R: 255, G: 165, B: 0, A: 255},
		{R: 147, G: 112, B: 219, A: 255},
		{R: 64, G: 224, B: 208, A: 255},
		{R: 220, G: 120, B: 60, A: 255},
		{R: 100, G: 149, B: 237, A: 255},
	}

	for by := 0; by*blockH < sliderBgHeight; by++ {
		for bx := 0; bx*blockW < sliderBgWidth; bx++ {
			base := palette[r.Intn(len(palette))]
			x0 := bx * blockW
			y0 := by * blockH
			x1 := x0 + blockW
			y1 := y0 + blockH
			for py := y0; py < y1 && py < sliderBgHeight; py++ {
				for px := x0; px < x1 && px < sliderBgWidth; px++ {
					v := int8(r.Intn(41) - 20)
					img.SetNRGBA(px, py, color.NRGBA{
						R: addVariation(base.R, v),
						G: addVariation(base.G, v),
						B: addVariation(base.B, v),
						A: 255,
					})
				}
			}
		}
	}

	// Add some random circles for visual complexity
	numCircles := 6 + r.Intn(6)
	for i := 0; i < numCircles; i++ {
		cx := r.Intn(sliderBgWidth)
		cy := r.Intn(sliderBgHeight)
		radius := 18 + r.Intn(30)
		circleColor := color.NRGBA{
			R: uint8(r.Intn(256)),
			G: uint8(r.Intn(256)),
			B: uint8(r.Intn(256)),
			A: 180,
		}
		drawCircle(img, cx, cy, radius, circleColor)
	}

	return img
}

func addVariation(base uint8, v int8) uint8 {
	result := int(base) + int(v)
	if result < 0 {
		return 0
	}
	if result > 255 {
		return 255
	}
	return uint8(result)
}

func drawCircle(img *image.NRGBA, cx, cy, radius int, c color.NRGBA) {
	bounds := img.Bounds()
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= radius*radius {
				if x >= bounds.Min.X && x < bounds.Max.X && y >= bounds.Min.Y && y < bounds.Max.Y {
					img.SetNRGBA(x, y, c)
				}
			}
		}
	}
}

func imageToPNGBase64(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
