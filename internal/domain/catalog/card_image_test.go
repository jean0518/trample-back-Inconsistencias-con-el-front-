package catalog

import (
	"strings"
	"testing"
)

// jpegBytes, pngBytes, gifBytes y webpBytes son los mínimos reconocibles por
// http.DetectContentType, que solo inspecciona los primeros 512 bytes.
func jpegBytes() []byte { return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 32)...) }
func pngBytes() []byte {
	return append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 32)...)
}
func gifBytes() []byte { return append([]byte("GIF89a"), make([]byte, 32)...) }
func webpBytes() []byte {
	return append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
}

func TestNewImageUploadAceptaLosFormatosPermitidos(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		wantType string
		wantExt  string
	}{
		{"jpeg", jpegBytes(), "image/jpeg", ".jpg"},
		{"png", pngBytes(), "image/png", ".png"},
		{"gif", gifBytes(), "image/gif", ".gif"},
		{"webp", webpBytes(), "image/webp", ".webp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upload, err := NewImageUpload(tt.content)
			if err != nil {
				t.Fatalf("debería aceptar %s: %v", tt.name, err)
			}
			if upload.ContentType != tt.wantType {
				t.Fatalf("content_type: esperaba %q, hay %q", tt.wantType, upload.ContentType)
			}
			if got := upload.Extension(); got != tt.wantExt {
				t.Fatalf("extensión: esperaba %q, hay %q", tt.wantExt, got)
			}
			if upload.SizeBytes() != len(tt.content) {
				t.Fatalf("tamaño: esperaba %d, hay %d", len(tt.content), upload.SizeBytes())
			}
			if upload.IsEmpty() {
				t.Fatal("una imagen válida no debería reportarse vacía")
			}
		})
	}
}

func TestNewImageUploadRechazaArchivoVacio(t *testing.T) {
	for _, content := range [][]byte{nil, {}} {
		if _, err := NewImageUpload(content); err == nil {
			t.Fatalf("un archivo vacío (%d bytes) debería rechazarse", len(content))
		}
	}
}

func TestNewImageUploadRechazaFormatoNoSoportado(t *testing.T) {
	tests := []struct {
		name        string
		content     []byte
		wantMessage string
	}{
		{"pdf", []byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"), "PDF"},
		{"zip", []byte("PK\x03\x04\x14\x00\x00\x00"), "ZIP"},
		{"texto", []byte("esto es un archivo de texto cualquiera y mas largo"), "texto"},
		// El caso que motiva la validación: un binario que se hace pasar por
		// una imagen. Sin el sniffer, el Content-Type del cliente decidiría.
		{"ejecutable", []byte{0x4D, 0x5A, 0x90, 0x00}, "binario"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewImageUpload(tt.content)
			if err == nil {
				t.Fatalf("%s debería rechazarse", tt.name)
			}
			// El mensaje debe nombrar el problema en palabras que el staff
			// entienda, no devolver el content_type crudo del sniffer.
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("esperaba que el error mencionara %q, hay %q", tt.wantMessage, err.Error())
			}
		})
	}
}

func TestNewImageUploadRechazaArchivoTDemasiadoGrande(t *testing.T) {
	// La cabecera PNG va primero para que el rechazo sea por tamaño y no por
	// formato, que es lo que se quiere comprobar.
	oversized := append(pngBytes(), make([]byte, MaxImageBytes)...)

	_, err := NewImageUpload(oversized)
	if err == nil {
		t.Fatal("un archivo sobre el máximo debería rechazarse")
	}
	if !strings.Contains(err.Error(), "5 MB") {
		t.Fatalf("el error debería decir cuál es el máximo, hay %q", err.Error())
	}
}

// El sniffer solo mira 512 bytes, así que un archivo con junk al final pero
// cabecera válida debe aceptarse: el límite es deliberado.
func TestNewImageUploadAceptaArchivosGrandesConCabeceraValida(t *testing.T) {
	big := append(pngBytes(), make([]byte, 1000)...)
	if len(big) <= sniffBufferSize {
		t.Fatalf("el archivo de prueba debería superar el recorte, mide %d", len(big))
	}

	upload, err := NewImageUpload(big)
	if err != nil {
		t.Fatalf("una imagen válida de cualquier tamaño debe aceptarse: %v", err)
	}
	if upload.ContentType != "image/png" {
		t.Fatalf("esperaba image/png, hay %q", upload.ContentType)
	}
}

func TestValidateImageSourceAceptaSoloUnaForma(t *testing.T) {
	urlOnly, err := ValidateImageSource("  https://cdn.ejemplo.com/a.jpg  ", ImageUpload{})
	if err != nil {
		t.Fatalf("una URL sola debería valer: %v", err)
	}
	if urlOnly.URL != "https://cdn.ejemplo.com/a.jpg" {
		t.Fatalf("esperaba la URL recortada, hay %q", urlOnly.URL)
	}
	if !urlOnly.HasImage() {
		t.Fatal("una URL no vacía cuenta como imagen")
	}

	uploadOnly, err := ValidateImageSource("", ImageUpload{Content: []byte{1}, ContentType: "image/png"})
	if err != nil {
		t.Fatalf("un archivo solo debería valer: %v", err)
	}
	if !uploadOnly.HasImage() {
		t.Fatal("un archivo no vacío cuenta como imagen")
	}
}

func TestValidateImageSourceRechazaAmbasFormas(t *testing.T) {
	_, err := ValidateImageSource("https://cdn.ejemplo.com/a.jpg", ImageUpload{Content: []byte{1}})
	if err == nil {
		t.Fatal("mandar URL y archivo a la vez es ambiguo y debe rechazarse")
	}
}

// El alta sin frontal es válida: la imagen es opcional.
func TestValidateImageSourceAceptaNingunaForma(t *testing.T) {
	src, err := ValidateImageSource("   ", ImageUpload{})
	if err != nil {
		t.Fatalf("no mandar imagen es válido: %v", err)
	}
	if src.HasImage() {
		t.Fatal("sin URL ni archivo no hay imagen")
	}
}
