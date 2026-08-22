package scrydex

import (
	"strings"
	"testing"
)

const pocketExclusion = `-expansion.series:"Pokémon Pocket"`

func TestBuildQueryName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		nameClause string
	}{
		{name: "una palabra usa wildcard de prefijo", input: "pikachu", nameClause: "name:pikachu*"},
		{name: "varias palabras van como frase sin comodin", input: "Rare Candy", nameClause: `name:"Rare Candy"`},
		// El índice de Scrydex conserva la puntuación dentro de los nombres:
		// "Team Rocket s Factory" o "Charizard GX" no matchean nada.
		{name: "apostrofe se conserva", input: "Team Rocket's Factory", nameClause: `name:"Team Rocket's Factory"`},
		{name: "apostrofe tipografico se normaliza", input: "Team Rocket’s Factory", nameClause: `name:"Team Rocket's Factory"`},
		{name: "una palabra con apostrofe usa wildcard", input: "Rocket's", nameClause: "name:Rocket's*"},
		{name: "guion interno se conserva", input: "Charizard-GX", nameClause: "name:Charizard-GX*"},
		{name: "guion suelto no genera operador de exclusion", input: "Team Rocket - Pikachu", nameClause: `name:"Team Rocket Pikachu"`},
		{name: "guion inicial se descarta", input: "-GX", nameClause: "name:GX*"},
		{name: "punto se conserva", input: "Mr. Mime", nameClause: `name:"Mr. Mime"`},
		{name: "ampersand se conserva", input: "Pikachu & Zekrom", nameClause: `name:"Pikachu & Zekrom"`},
		{name: "comillas dobles se eliminan", input: `"pikachu"`, nameClause: "name:pikachu*"},
		{name: "dos puntos se eliminan", input: "Time: Garchomp", nameClause: `name:"Time Garchomp"`},
		{name: "solo simbolos produce clausula vacia", input: "'-:", nameClause: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildQuery("pokemon", tt.input, "", "", "", "")
			want := strings.TrimSpace(tt.nameClause + " " + pocketExclusion)
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestBuildQueryMantieneOtrosFiltros(t *testing.T) {
	got := buildQuery("pokemon", "Team Rocket's Factory", "me2pt5", "Rare", "", "Trainer")
	for _, want := range []string{
		`name:"Team Rocket's Factory"`,
		"expansion.id:me2pt5",
		"rarity:Rare",
		`supertype:"Trainer"`,
		pocketExclusion,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("query %q no contiene %q", got, want)
		}
	}
}

func TestFallbackSinSimbolosDifiereDelPrimario(t *testing.T) {
	primary := buildQuery("pokemon", "pikachu.", "", "", "", "")
	fallback := buildQuery("pokemon", stripNameSymbols("pikachu."), "", "", "", "")

	wantPrimary := "name:pikachu.* " + pocketExclusion
	if primary != wantPrimary {
		t.Fatalf("primario got %q, want %q", primary, wantPrimary)
	}
	wantFallback := "name:pikachu* " + pocketExclusion
	if fallback != wantFallback {
		t.Fatalf("fallback got %q, want %q", fallback, wantFallback)
	}
}

func TestStripNameSymbolsReduceALetrasYDigitos(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "pikachu.", want: "pikachu"},
		{input: "Team Rocket's", want: "Team Rocket s"},
		{input: "Charizard-GX", want: "Charizard GX"},
	}
	for _, tt := range tests {
		if got := strings.TrimSpace(stripNameSymbols(tt.input)); got != tt.want {
			t.Fatalf("stripNameSymbols(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildQueryMTGUsaTypes(t *testing.T) {
	got := buildQuery("mtg", "", "", "", "Instant", "Creature")
	if !strings.Contains(got, "types:Instant") || strings.Contains(got, "supertype:") {
		t.Fatalf("mtg debe usar types: e ignorar supertype, got %q", got)
	}
}

func TestBuildQueryPokemonSinSupertype(t *testing.T) {
	got := buildQuery("pokemon", "mewtwo", "", "", "", "")
	if strings.Contains(got, "supertype:") {
		t.Fatalf("no debe incluir supertype vacío, got %q", got)
	}
}
