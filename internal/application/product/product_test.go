package product

import (
	"context"
	"errors"
	"testing"

	"trample-back/internal/domain/catalog"
	"trample-back/internal/domain/product"
)

// fakeRepo es un ProductRepository en memoria con el mismo tope de fotos que
// el real.
type fakeRepo struct {
	products map[int64]*product.Product
	nextID   int64
	failURL  string
	failUp   bool
	sold     map[int64]bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{products: map[int64]*product.Product{}, sold: map[int64]bool{}}
}

func (f *fakeRepo) Create(_ context.Context, _ string, in product.Input, status string) (product.Product, error) {
	f.nextID++
	p := &product.Product{ID: f.nextID, Title: in.Title, Description: in.Description, PriceCOP: in.PriceCOP, Stock: in.Stock, Status: status, Language: in.Language, Game: in.Game}
	f.products[p.ID] = p
	return *p, nil
}
func (f *fakeRepo) Update(_ context.Context, id int64, in product.Input) (product.Product, error) {
	p, ok := f.products[id]
	if !ok {
		return product.Product{}, product.ErrNotFound
	}
	p.Title, p.Description, p.PriceCOP, p.Stock = in.Title, in.Description, in.PriceCOP, in.Stock
	p.Language, p.Game = in.Language, in.Game
	return *p, nil
}
func (f *fakeRepo) SetStatus(_ context.Context, id int64, status string) (product.Product, error) {
	p, ok := f.products[id]
	if !ok {
		return product.Product{}, product.ErrNotFound
	}
	p.Status = status
	return *p, nil
}
func (f *fakeRepo) BulkUpdate(_ context.Context, ids []int64, status, game *string) (int, error) {
	updated := 0
	for _, id := range ids {
		p, ok := f.products[id]
		if !ok {
			continue
		}
		if status != nil {
			p.Status = *status
		}
		if game != nil {
			p.Game = *game
		}
		updated++
	}
	return updated, nil
}
func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	if f.sold[id] {
		return product.ErrHasSales
	}
	if _, ok := f.products[id]; !ok {
		return product.ErrNotFound
	}
	delete(f.products, id)
	return nil
}
func (f *fakeRepo) GetByID(_ context.Context, id int64) (product.Product, error) {
	p, ok := f.products[id]
	if !ok {
		return product.Product{}, product.ErrNotFound
	}
	return *p, nil
}
func (f *fakeRepo) List(_ context.Context, flt product.ListFilter) ([]product.Product, int, error) {
	var out []product.Product
	for _, p := range f.products {
		if !flt.IncludeHidden && p.Status != product.StatusActive {
			continue
		}
		out = append(out, *p)
	}
	return out, len(out), nil
}
func (f *fakeRepo) add(id int64, url string) (product.Image, error) {
	p, ok := f.products[id]
	if !ok {
		return product.Image{}, product.ErrNotFound
	}
	if len(p.Images) >= product.MaxImages {
		return product.Image{}, product.ErrTooManyImages
	}
	img := product.Image{ID: int64(len(p.Images) + 1), Position: len(p.Images) + 1, URL: url}
	p.Images = append(p.Images, img)
	return img, nil
}
func (f *fakeRepo) AddImageURL(_ context.Context, id int64, url string) (product.Image, error) {
	if url == f.failURL && url != "" {
		return product.Image{}, errors.New("boom")
	}
	return f.add(id, url)
}
func (f *fakeRepo) AddImageUpload(_ context.Context, id int64, _ catalog.ImageUpload) (product.Image, error) {
	if f.failUp {
		return product.Image{}, errors.New("boom")
	}
	return f.add(id, "/product-images/x")
}
func (f *fakeRepo) RemoveImage(context.Context, int64, int64) error { return nil }
func (f *fakeRepo) GetImage(context.Context, int64) (string, []byte, error) {
	return "", nil, product.ErrImageNotFound
}

var png = catalog.ImageUpload{Content: []byte{1}, ContentType: "image/png"}

func validCreate() CreateInput {
	return CreateInput{Title: "Caja Pokémon", PriceCOP: 450000, Stock: 2}
}

func TestCreateDefaultsToActiveAndTrims(t *testing.T) {
	uc := NewManageUseCase(newFakeRepo())
	in := validCreate()
	in.Title = "  Caja Pokémon  "
	p, err := uc.Create(context.Background(), "admin-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Caja Pokémon" || p.Status != product.StatusActive {
		t.Errorf("producto: %+v", p)
	}
}

func TestCreateRejectsInvalidBeforeWriting(t *testing.T) {
	repo := newFakeRepo()
	uc := NewManageUseCase(repo)

	bad := []CreateInput{
		{Title: "", PriceCOP: 1000},
		{Title: "x", PriceCOP: 0},
		{Title: "x", PriceCOP: 1000, Status: "otro"},
		{Title: "x", PriceCOP: 1000, ImageURLs: []string{"ftp://x/y.jpg"}},
		{Title: "x", PriceCOP: 1000, ImageURLs: []string{"a", "b", "c"}, Uploads: []catalog.ImageUpload{png, png, png}},
	}
	for i, in := range bad {
		if _, err := uc.Create(context.Background(), "admin-1", in); err == nil {
			t.Errorf("caso %d: esperaba error", i)
		}
	}
	if len(repo.products) != 0 {
		t.Errorf("no debía crear nada ante datos inválidos, hay %d", len(repo.products))
	}
	if _, err := uc.Create(context.Background(), "", validCreate()); err == nil {
		t.Error("sin admin debía fallar")
	}
}

func TestCreateWithFivePhotosOK_SixRejected(t *testing.T) {
	uc := NewManageUseCase(newFakeRepo())
	in := validCreate()
	in.ImageURLs = []string{"https://a/1.jpg", "https://a/2.jpg"}
	in.Uploads = []catalog.ImageUpload{png, png, png}
	p, err := uc.Create(context.Background(), "admin-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Images) != 5 || p.Images[0].URL != "https://a/1.jpg" {
		t.Errorf("fotos: %+v", p.Images)
	}

	in.Uploads = append(in.Uploads, png)
	if _, err := uc.Create(context.Background(), "admin-1", in); !errors.Is(err, product.ErrTooManyImages) {
		t.Errorf("6 fotos: %v", err)
	}
}

func TestCreateImageFailureReturnsCreatedProduct(t *testing.T) {
	repo := newFakeRepo()
	repo.failUp = true
	uc := NewManageUseCase(repo)
	in := validCreate()
	in.ImageURLs = []string{"https://a/1.jpg"}
	in.Uploads = []catalog.ImageUpload{png}

	p, err := uc.Create(context.Background(), "admin-1", in)
	if !errors.Is(err, product.ErrImageAttachment) {
		t.Fatalf("esperaba ErrImageAttachment, got %v", err)
	}
	if p.ID == 0 || len(p.Images) != 1 {
		t.Errorf("debía devolver el producto con la foto que sí quedó: %+v", p)
	}
}

func TestPublicNeverSeesHidden(t *testing.T) {
	repo := newFakeRepo()
	manage := NewManageUseCase(repo)
	query := NewQueryUseCase(repo)
	ctx := context.Background()

	p, _ := manage.Create(ctx, "admin-1", validCreate())
	if _, err := manage.SetStatus(ctx, p.ID, product.StatusHidden); err != nil {
		t.Fatal(err)
	}

	if _, err := query.Get(ctx, p.ID, false); !errors.Is(err, product.ErrNotFound) {
		t.Errorf("público debía recibir ErrNotFound, got %v", err)
	}
	if _, err := query.Get(ctx, p.ID, true); err != nil {
		t.Errorf("admin debía verlo: %v", err)
	}
	if items, _, _ := query.List(ctx, product.ListFilter{IncludeHidden: true}, false); len(items) != 0 {
		t.Errorf("público no puede pedir ocultos: %d", len(items))
	}
	if items, _, _ := query.List(ctx, product.ListFilter{IncludeHidden: true}, true); len(items) != 1 {
		t.Errorf("admin debía listar el oculto: %d", len(items))
	}
	if _, err := manage.SetStatus(ctx, p.ID, "borrado"); err == nil {
		t.Error("estado inválido debía fallar")
	}
}

func TestDeleteWithSalesIsRejected(t *testing.T) {
	repo := newFakeRepo()
	uc := NewManageUseCase(repo)
	p, _ := uc.Create(context.Background(), "admin-1", validCreate())
	repo.sold[p.ID] = true
	if err := uc.Delete(context.Background(), p.ID); !errors.Is(err, product.ErrHasSales) {
		t.Errorf("got %v", err)
	}
}

func TestBulkUpdateStatusAndGame(t *testing.T) {
	repo := newFakeRepo()
	uc := NewManageUseCase(repo)
	ctx := context.Background()
	a, _ := uc.Create(ctx, "admin-1", validCreate())
	b, _ := uc.Create(ctx, "admin-1", validCreate())

	status, game := product.StatusHidden, product.GamePokemon
	updated, err := uc.BulkUpdate(ctx, []int64{a.ID, b.ID}, &status, &game)
	if err != nil || updated != 2 {
		t.Fatalf("bulk: %d %v", updated, err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		p, _ := repo.GetByID(ctx, id)
		if p.Status != product.StatusHidden || p.Game != product.GamePokemon {
			t.Errorf("producto %d quedó %+v", id, p)
		}
	}

	if _, err := uc.BulkUpdate(ctx, nil, nil, nil); err == nil {
		t.Error("sin ids debía fallar")
	}
	bad := "otro"
	if _, err := uc.BulkUpdate(ctx, []int64{a.ID}, nil, &bad); err == nil {
		t.Error("juego inválido debía fallar")
	}
}

func TestUpdateValidates(t *testing.T) {
	repo := newFakeRepo()
	uc := NewManageUseCase(repo)
	p, _ := uc.Create(context.Background(), "admin-1", validCreate())

	if _, err := uc.Update(context.Background(), p.ID, product.Input{Title: "", PriceCOP: 1}); err == nil {
		t.Error("título vacío debía fallar")
	}
	got, err := uc.Update(context.Background(), p.ID, product.Input{Title: " Nuevo ", PriceCOP: 5000, Stock: 0})
	if err != nil || got.Title != "Nuevo" || !got.SoldOut() {
		t.Errorf("update: %+v %v", got, err)
	}
}
