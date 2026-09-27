package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/ports/out"
)

// fakeExpansionRepo registra lo que se le pide crear.
type fakeExpansionRepo struct {
	out.ExpansionRepository
	gotGameCode string
	got         catalog.Expansion
	result      catalog.Expansion
	created     bool
	err         error
	calls       int
}

func (f *fakeExpansionRepo) CreateManual(_ context.Context, gameCode string, e catalog.Expansion) (catalog.Expansion, bool, error) {
	f.calls++
	f.gotGameCode = gameCode
	f.got = e
	return f.result, f.created, f.err
}

func TestCreateExpansionDerivaElExternalIDDelNombre(t *testing.T) {
	repo := &fakeExpansionRepo{result: catalog.Expansion{ID: 12, Name: "Base Set"}, created: true}
	uc := NewNewExpansionUseCase(repo)

	got, err := uc.Execute(context.Background(), catalog.NewExpansion{
		GameCode: "pokemon",
		Name:     "  Base Set  ",
		Code:     "BS",
		Total:    102,
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	// El identificador se deriva del nombre normalizado, no del texto con
	// espacios que escribió el staff.
	if repo.got.ExternalID != "manual:base-set" {
		t.Fatalf("esperaba manual:base-set, hay %q", repo.got.ExternalID)
	}
	if repo.gotGameCode != "pokemon" {
		t.Fatalf("esperaba el juego pokemon, hay %q", repo.gotGameCode)
	}
	if !got.Created {
		t.Fatal("created debe ser true cuando la expansión no existía")
	}
	if got.Expansion.ID != 12 {
		t.Fatalf("esperaba la expansión devuelta por el repositorio, hay %d", got.Expansion.ID)
	}
}

// El botón "+" puede pulsarse dos veces sin que se duplique el set: la segunda
// vez el repositorio devuelve la fila existente con created=false.
func TestCreateExpansionEsIdempotente(t *testing.T) {
	repo := &fakeExpansionRepo{
		result:  catalog.Expansion{ID: 12, Name: "Base Set"},
		created: false,
	}
	uc := NewNewExpansionUseCase(repo)

	got, err := uc.Execute(context.Background(), catalog.NewExpansion{GameCode: "pokemon", Name: "Base Set"})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if got.Created {
		t.Fatal("created debe ser false cuando la expansión ya existía")
	}
	if got.Expansion.ID != 12 {
		t.Fatalf("debe devolver la expansión existente, hay %d", got.Expansion.ID)
	}
}

func TestCreateExpansionRechazaDatosIncompletos(t *testing.T) {
	tests := []struct {
		name    string
		input   catalog.NewExpansion
		wantErr string
	}{
		{"sin juego", catalog.NewExpansion{Name: "Base Set"}, "juego"},
		{"sin nombre", catalog.NewExpansion{GameCode: "pokemon"}, "nombre de la expansión"},
		{"total negativo", catalog.NewExpansion{GameCode: "pokemon", Name: "Base Set", Total: -5}, "negativo"},
		{"fecha imposible", catalog.NewExpansion{GameCode: "pokemon", Name: "Base Set", ReleaseDate: "ayer"}, "fecha"},
		// Se rechaza en vez de adivinar: "06-09-2024" puede leerse como 6 de
		// septiembre o como 9 de junio, y elegir una sería una lotería.
		{"fecha ambigua con guiones", catalog.NewExpansion{GameCode: "pokemon", Name: "Base Set", ReleaseDate: "06-09-2024"}, "fecha"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeExpansionRepo{}
			uc := NewNewExpansionUseCase(repo)

			_, err := uc.Execute(context.Background(), tt.input)
			if err == nil {
				t.Fatal("se esperaba un error de validación")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("esperaba un error que mencionara %q, hay %q", tt.wantErr, err.Error())
			}
			if repo.calls != 0 {
				t.Fatal("no se debe llamar al repositorio con datos inválidos")
			}
		})
	}
}

// Scrydex guarda las fechas como texto "AAAA-MM-DD" y los filtros por año del
// catálogo comparan contra ese formato, así que una fecha con barras se
// normaliza en vez de guardarse tal cual.
func TestCreateExpansionNormalizaLaFechaDeLanzamiento(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"2024-09-06", "2024-09-06"},
		{"06/09/2024", "2024-09-06"},
		{"  2024-09-06  ", "2024-09-06"},
	}

	for _, tt := range tests {
		repo := &fakeExpansionRepo{}
		uc := NewNewExpansionUseCase(repo)

		if _, err := uc.Execute(context.Background(), catalog.NewExpansion{GameCode: "pokemon", Name: "Base Set", ReleaseDate: tt.in}); err != nil {
			t.Fatalf("%q no debería fallar: %v", tt.in, err)
		}
		if repo.got.ReleaseDate != tt.want {
			t.Fatalf("%q: esperaba %q, hay %q", tt.in, tt.want, repo.got.ReleaseDate)
		}
	}
}

func TestCreateExpansionPropagaElErrorDelRepositorio(t *testing.T) {
	repo := &fakeExpansionRepo{err: errors.New("juego no encontrado")}
	uc := NewNewExpansionUseCase(repo)

	if _, err := uc.Execute(context.Background(), catalog.NewExpansion{GameCode: "nope", Name: "X"}); err == nil {
		t.Fatal("se esperaba el error del repositorio")
	}
}

// El botón "+" y el alta manual comparten la derivación del external_id de la
// expansión. Si difirieran, el botón crearía un set y el alta manual colgaría la
// carta de otro, así que la entrada tiene que quedar normalizada igual en ambos
// caminos.
func TestElAltaManualNormalizaElNombreIgualQueElBotonMas(t *testing.T) {
	card, err := catalog.ManualCard{
		GameCode:      "pokemon",
		ExpansionName: "  Neo Genesis  ",
		Name:          "Dark Blastoise",
	}.Validate()
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if card.ExpansionName != "Neo Genesis" {
		t.Fatalf("esperaba el nombre recortado, hay %q", card.ExpansionName)
	}

	// El repositorio del alta manual deriva el id con esta misma función, así
	// que ambos caminos convergen en la misma fila de expansions.
	viaAltaManual, err := catalog.ExpansionExternalIDForManual(card.ExpansionName)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	viaBotonMas, err := catalog.ExpansionExternalIDForManual(" Neo Genesis ")
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if viaAltaManual != viaBotonMas {
		t.Fatalf("los dos caminos deben derivar el mismo id: %q vs %q", viaAltaManual, viaBotonMas)
	}
}
