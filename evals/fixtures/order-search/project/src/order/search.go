// Package order は注文に関する処理。
package order

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// SearchRequest は注文履歴検索の入力。
type SearchRequest struct {
	From        *time.Time
	To          *time.Time
	ProductName string
}

// OrderSummary は検索結果の1行。
type OrderSummary struct {
	OrderNo     string
	OrderedAt   time.Time
	ProductName string
	Total       int
}

// Store は注文の保存先。
type Store interface {
	FindOrders(ctx context.Context, memberID string, since time.Time, req SearchRequest, limit int) ([]OrderSummary, error)
}

// ErrNotFound は該当なし。
var ErrNotFound = errors.New("E-404")

const (
	searchLimit   = 15
	searchTimeout = 10 * time.Second
	retention     = 12 // か月
)

// Search は会員本人の注文履歴を新しい順に返す。
func Search(ctx context.Context, st Store, memberID string, req SearchRequest) ([]OrderSummary, error) {
	if memberID == "" {
		return nil, errors.New("unauthorized")
	}
	if len([]rune(req.ProductName)) > 100 {
		return nil, errors.New("E-400")
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	since := time.Now().AddDate(0, -retention, 0)
	rows, err := st.FindOrders(ctx, memberID, since, req, searchLimit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows, nil
}

// StatusOf は検索エラーを HTTP ステータスに変換する。
func StatusOf(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
