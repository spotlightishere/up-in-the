package main

import (
	"fmt"
	"image"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	// Acceptable image formats
	"golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

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

func error(w http.ResponseWriter) {
	w.WriteHeader(500)
	w.Write([]byte("it broke"))
}

func main() {
	global = make(map[string]image.Image)
	http.HandleFunc("/", primaryHandler)
	http.HandleFunc("/img", imageHandler)

	log.Println(http.ListenAndServeTLS("[::]:443", "cert.pem", "key.pem", http.DefaultServeMux))
}

func primaryHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		page := fmt.Sprintf(`
<!DOCTYPE html>
<html>
	<head>
		<title>puuuush</title>
	</head>
	<body>
		<form action="/" method="post" enctype="multipart/form-data">
		  <input type="file" name="fileToUpload" id="fileToUpload">
		  <input type="hidden" name="token" id="token" value="%s">
		  <br>
		  <input type="submit" value="Upload Image" name="submit">
		</form>
	</body>
</html>
`, RandStringBytesMaskImprSrcSB(12))

		w.Write([]byte(page))
	case "POST":
		file, _, err := r.FormFile("fileToUpload")
		if err != nil {
			error(w)
			return
		}

		token := r.FormValue("token")
		if token == "" {
			error(w)
			return
		}

		// Interpret image
		img, _, err := image.Decode(file)
		if err != nil {
			error(w)
			return
		}
		global[token] = img

		pusher, ok := w.(http.Pusher)
		if !ok {
			error(w)
			return
		}

		body := `
<!DOCTYPE html>
<html>
	<head>
		<title>puuuush</title>
		<style>
			body {
				line-height: 0px;
				font-size: 0px;
			}
		</style>
	</head>
	<body>
`

		// Run through the y axis so we can create a new line on images.
		maxX := img.Bounds().Max.X
		maxY := img.Bounds().Max.Y

		for y := 0; y < maxY; y++ {
			// Register all X positions possible for this area.
			for x := 0; x < maxX; x++ {
				url := fmt.Sprintf("/img?x=%d&y=%d&token=%s", x, y, token)
				body += fmt.Sprintf("<img src='%s'>", url)

				if err := pusher.Push(url, nil); err != nil {
					log.Printf("Failed to push: %v", err)
				}
			}

			// And now, a newline.
			body += "<br>"
		}

		// And we're done.
		body += `
	</body>
</html>
`
		log.Print("Sending...")
		w.Write([]byte(body))

		// We've now pushed everything.
		delete(global, token)

	default:
		w.Write([]byte("Were you expecting something?"))
	}
	return
}

func imageHandler(w http.ResponseWriter, r *http.Request) {
	queries := r.URL.Query()
	xS := queries["x"][0]
	x, err := strconv.Atoi(xS)
	if err != nil {
		error(w)
		return
	}

	yS := queries["y"][0]
	y, err := strconv.Atoi(yS)
	if err != nil {
		error(w)
		return
	}

	token := queries["token"][0]
	if token == "" {
		error(w)
		return
	}

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
	img.Set(0, 0, global[token].At(x, y))

	// I truly am sorry.
	w.Header().Set("Content-Type", "image/bmp")
	log.Println(y)
	bmp.Encode(w, img)
	return
}
