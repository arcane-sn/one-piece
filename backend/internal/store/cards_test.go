package store

import (
	"reflect"
	"strings"
	"testing"
)

// col returns "title:position" for each card in a status, in list order.
func col(t *testing.T, s *Store, boardID int64, status string) []string {
	t.Helper()
	cs, err := s.ListCards(boardID)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, c := range cs {
		if c.Status == status {
			out = append(out, c.Title+":"+string(rune('0'+c.Position)))
		}
	}
	return out
}

func threeCards(t *testing.T) (*Store, int64, []Card) {
	t.Helper()
	s := openTemp(t)
	b, err := s.CreateBoard("b", nil)
	if err != nil {
		t.Fatal(err)
	}
	var cs []Card
	for _, title := range []string{"a", "b", "c"} {
		c, err := s.CreateCard(b.ID, title, "")
		if err != nil {
			t.Fatal(err)
		}
		cs = append(cs, c)
	}
	return s, b.ID, cs
}

func TestCreateCardStartsInBusinessAtEnd(t *testing.T) {
	s, bid, cs := threeCards(t)
	if cs[2].Status != "business" || cs[2].Position != 2 {
		t.Errorf("third card = %+v", cs[2])
	}
	if got := col(t, s, bid, "business"); !reflect.DeepEqual(got, []string{"a:0", "b:1", "c:2"}) {
		t.Errorf("business = %v", got)
	}
}

func TestCardTitleRejectsBlankAndTooLong(t *testing.T) {
	s, bid, cs := threeCards(t)
	for _, title := range []string{"", "   ", strings.Repeat("x", 201)} {
		if _, err := s.CreateCard(bid, title, ""); !isValidation(err) {
			t.Errorf("create %q: want ValidationError, got %v", title, err)
		}
		if _, err := s.UpdateCard(cs[0].ID, title, ""); !isValidation(err) {
			t.Errorf("update %q: want ValidationError, got %v", title, err)
		}
	}
}

func TestMoveAcrossColumnsKeepsPositionsContiguous(t *testing.T) {
	s, bid, cs := threeCards(t)
	if _, err := s.MoveCard(cs[1].ID, "agent", 0); err != nil {
		t.Fatal(err)
	}
	if got := col(t, s, bid, "business"); !reflect.DeepEqual(got, []string{"a:0", "c:1"}) {
		t.Errorf("business = %v", got)
	}
	if got := col(t, s, bid, "agent"); !reflect.DeepEqual(got, []string{"b:0"}) {
		t.Errorf("agent = %v", got)
	}
}

func TestMoveWithinColumnReorders(t *testing.T) {
	s, bid, cs := threeCards(t)
	if _, err := s.MoveCard(cs[0].ID, "business", 2); err != nil {
		t.Fatal(err)
	}
	if got := col(t, s, bid, "business"); !reflect.DeepEqual(got, []string{"b:0", "c:1", "a:2"}) {
		t.Errorf("business = %v", got)
	}
}

func TestMovePositionClamped(t *testing.T) {
	s, bid, cs := threeCards(t)
	s.MoveCard(cs[2].ID, "agent", -5)
	s.MoveCard(cs[0].ID, "agent", 999)
	if got := col(t, s, bid, "agent"); !reflect.DeepEqual(got, []string{"c:0", "a:1"}) {
		t.Errorf("agent = %v", got)
	}
}

func TestMoveUnknownStatusRejected(t *testing.T) {
	s, _, cs := threeCards(t)
	if _, err := s.MoveCard(cs[0].ID, "archived", 0); !isValidation(err) {
		t.Errorf("want ValidationError, got %v", err)
	}
	if _, err := s.MoveCard(9999, "agent", 0); err != ErrNotFound {
		t.Errorf("missing card: %v, want ErrNotFound", err)
	}
}

func TestCreateCardOnMissingBoardIsNotFound(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateCard(42, "x", ""); err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
