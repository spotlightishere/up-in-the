package main

import (
	"embed"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	// Acceptable image formats
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	"golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// The hashmap we store images in temporarily.
var global map[string]image.Image

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

// RandStringBytesMaskImprSrcSB is from the legitimately wonderful https://stackoverflow.com/a/31832326
func RandStringBytesMaskImprSrcSB(n int) string {
	sb := strings.Builder{}
	sb.Grow(n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}

var (
	ErrNoFile       = errors.New("no file provided")
	ErrFailedRead   = errors.New("unable to open reader")
	ErrInvalidImage = errors.New("invalid image type")
	ErrNoToken      = errors.New("no token was provided")
)

// PushData holds information the push template can utilize.
type PushData struct {
	XRange []struct{}
	YRange []struct{}
	Token  string
}

func error(w http.ResponseWriter) {
	w.WriteHeader(500)
	w.Write([]byte("it broke"))
}

// Templates holds our template data.
//go:embed templates
var Templates embed.FS

// PushTemplate is the template we'll use for pushed data.
var PushTemplate = template.Must(template.ParseFS(Templates, "templates/push.html"))

func main() {
	// The hashmap we'll store images in.
	global = make(map[string]image.Image)

	r := gin.Default()
	r.GET("/", indexHandler)

	// We'll use this template when pushing.
	r.SetHTMLTemplate(PushTemplate)
	r.POST("/", pushHandler)

	// Image handling methods
	r.GET("/img", imageHandler)
	r.GET("/delete", deleteHandler)

	r.RunTLS("[::]:443", "cert.pem", "key.pem")
}

// indexHandler serves our index.html.
func indexHandler(c *gin.Context) {
	c.FileFromFS("templates/", http.FS(Templates))
}

// pushHandler handles pushing our file over HTTP/2.
func pushHandler(c *gin.Context) {
	// Determine our file.
	file, err := c.FormFile("fileToUpload")
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, ErrNoFile)
		return
	}

	// Generate a unique token to reference this request with.
	token := RandStringBytesMaskImprSrcSB(12)

	// Interpret image.
	fileReader, err := file.Open()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, ErrFailedRead)
		return
	}
	img, _, err := image.Decode(fileReader)
	if err == image.ErrFormat {
		c.AbortWithError(http.StatusBadRequest, ErrInvalidImage)
		return
	} else if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Store this image in our current hashmap.
	global[token] = img

	// Attempt to get a pusher.
	pusher := c.Writer.Pusher()
	if pusher == nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Obtain the dimensions of this image.
	maxX := img.Bounds().Max.X
	maxY := img.Bounds().Max.Y

	// Create integers of this length.
	// We won't actually store anything in these.
	// Instead, we'll abuse these to iterate.
	valueX := make([]struct{}, maxX)
	valueY := make([]struct{}, maxY)

	// Push what we can.
	for y, _ := range valueY {
		for x, _ := range valueX {
			url := fmt.Sprintf("/img?x=%d&y=%d&token=%s", x, y, token)

			pusher.Push()
			if err = pusher.Push(url, nil); err != nil {
				// It's not worth logging.
				c.AbortWithError(http.StatusInternalServerError, err)
				return
			}
		}
	}

	// Finally, push our deletion image.
	url := fmt.Sprintf("/delete?token=%s", token)
	if err = pusher.Push(url, nil); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Render our template at long last!
	pushData := PushData{
		XRange: valueX,
		YRange: valueY,
		Token:  token,
	}
	c.HTML(http.StatusOK, "templates/push.html", pushData)
}

// imageHandler returns an individual pixel at the given location.
func imageHandler(c *gin.Context) {
	xS := c.Query("x")
	x, err := strconv.Atoi(xS)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	yS := c.Query("y")
	y, err := strconv.Atoi(yS)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	token := c.Query("token")
	if token == "" {
		c.AbortWithError(http.StatusBadRequest, ErrNoToken)
		return
	}

	globalImage, hasImage := global[token]

	// I am so sorry.
	img := image.NewRGBA(image.Rectangle{
		Min: image.Point{
			X: 0,
			Y: 0,
		},
		Max: image.Point{
			X: 1,
			Y: 1,
		},
	})

	// Sometimes it fails. lol
	if hasImage {
		img.Set(0, 0, globalImage.At(x, y))
	} else {
		img.Set(0, 0, color.Transparent)
	}

	// I truly am sorry.
	c.Header("Content-Type", "image/bmp")
	bmp.Encode(c.Writer, img)
}

func deleteHandler(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.AbortWithError(http.StatusBadRequest, ErrNoToken)
		return
	}

	delete(global, token)

	img := image.NewRGBA(image.Rectangle{
		Min: image.Point{
			X: 0,
			Y: 0,
		},
		Max: image.Point{
			X: 1,
			Y: 1,
		},
	})
	img.Set(0, 0, color.Transparent)

	c.Header("Content-Type", "image/png")
	png.Encode(c.Writer, img)
}
