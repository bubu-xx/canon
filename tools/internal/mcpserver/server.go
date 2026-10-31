// Package mcpserver は Canon を他の AI から読むための MCP サーバー（stdio）。
package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/bubu-xx/canon/tools/internal/check"
	"github.com/bubu-xx/canon/tools/internal/drift"
	"github.com/bubu-xx/canon/tools/internal/gaps"
	"github.com/bubu-xx/canon/tools/internal/graph"
	"github.com/bubu-xx/canon/tools/internal/model"
	"github.com/bubu-xx/canon/tools/internal/project"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server は MCP サーバー。
type Server struct {
	Version    string
	SchemaFlag string
	Start      string // 対象プロジェクトを探し始める場所
}

// Serve は in から要求を読み、out に応答を書く。
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // 通知には応答しない
		}
		res, rerr := s.handle(req)
		resp := response{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		pv := p.ProtocolVersion
		if pv == "" {
			pv = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "canon", "version": s.Version},
			"instructions":    "Canon はプロジェクトの検証済みの正解情報。値はすべて出典付き。unknown / conflict の項目は推測で補わず、質問の回答を待つこと。",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		out, err := s.call(p.Name, p.Arguments)
		if err != nil {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}, nil
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}

func toolList() []map[string]any {
	obj := func(props map[string]any, req ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(req) > 0 {
			m["required"] = req
		}
		return m
	}
	str := map[string]any{"type": "string"}
	strs := map[string]any{"type": "array", "items": str}
	return []map[string]any{
		{"name": "query", "description": "Canon の要素を ID・型・状態・文字列で検索する。値はすべて出典付き。",
			"inputSchema": obj(map[string]any{"ids": strs, "type": str, "status": map[string]any{"type": "string", "enum": []string{"confirmed", "unknown", "conflict"}}, "text": str, "limit": map[string]any{"type": "integer"}})},
		{"name": "impact", "description": "ある要素を変えたときに見直しが要る要素（参照元を推移的にたどる）を返す。",
			"inputSchema": obj(map[string]any{"id": str}, "id")},
		{"name": "gaps", "description": "空白（unknown）・矛盾（conflict）・リンク不足・未回答の質問を返す。",
			"inputSchema": obj(map[string]any{"blocking_only": map[string]any{"type": "boolean"}})},
		{"name": "context_pack", "description": "課題に着手する前の材料一式。対象要素と関連要素、止まるべき条件（必須の空白・矛盾）、未回答の質問、実装とのズレを返す。ids か課題文 text を渡す。",
			"inputSchema": obj(map[string]any{"ids": strs, "text": str})},
		{"name": "validate", "description": "Canon 全体を規則に照らして検査する。",
			"inputSchema": obj(map[string]any{})},
	}
}

func (s *Server) load() (*project.Project, error) {
	start := s.Start
	if start == "" {
		start = os.Getenv("CLAUDE_PROJECT_DIR")
	}
	if start == "" {
		start, _ = os.Getwd()
	}
	root, err := project.FindRoot(start)
	if err != nil {
		return nil, err
	}
	return project.Load(root, project.SchemaDir(s.SchemaFlag))
}

func (s *Server) call(name string, raw json.RawMessage) (any, error) {
	p, err := s.load()
	if err != nil {
		return nil, err
	}
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	switch name {
	case "query":
		return Query(p, strList(args["ids"]), strArg(args["type"]), strArg(args["status"]), strArg(args["text"]), intArg(args["limit"], 50)), nil
	case "impact":
		id := strArg(args["id"])
		if _, ok := p.Elements[id]; !ok {
			return nil, fmt.Errorf("要素 %s は Canon にありません", id)
		}
		return map[string]any{"id": id, "impacted": graph.Build(p).Impact(id)}, nil
	case "gaps":
		b, _ := args["blocking_only"].(bool)
		return gaps.Build(p, b), nil
	case "context_pack":
		return ContextPack(p, strList(args["ids"]), strArg(args["text"])), nil
	case "validate":
		is := check.Run(p, check.Options{})
		return map[string]any{"errors": check.Errors(is), "issues": is}, nil
	}
	return nil, fmt.Errorf("不明なツール %s", name)
}

// Compact は要素を受け渡し用に整える。
type Compact struct {
	ID     string                    `json:"id"`
	Type   string                    `json:"type"`
	Name   string                    `json:"name"`
	Fields map[string]map[string]any `json:"fields"`
	Links  []map[string]string       `json:"links,omitempty"`
}

func compact(e *model.Element) Compact {
	c := Compact{ID: e.ID, Type: e.Type, Name: e.Name, Fields: map[string]map[string]any{}}
	for _, n := range e.FieldOrder {
		f := e.Fields[n]
		m := map[string]any{"status": f.Status}
		switch f.Status {
		case model.StatusConfirmed:
			m["value"] = f.Value
			if f.Unit != "" {
				m["unit"] = f.Unit
			}
			m["source"] = f.Source.String()
		case model.StatusNA:
			m["source"] = f.Source.String()
			m["note"] = "この要素には当てはまらない（根拠付きの判断）"
		case model.StatusUnknown:
			m["question"] = f.Question
			m["note"] = "未確定。推測で補わないこと"
		case model.StatusConflict:
			var cl []map[string]any
			for _, x := range f.Claims {
				cl = append(cl, map[string]any{"value": x.Value, "source": x.Source.String()})
			}
			m["claims"] = cl
			m["question"] = f.Question
			m["note"] = "情報源が食い違っている。どちらも勝手に選ばないこと"
		}
		c.Fields[n] = m
	}
	for _, l := range e.Links {
		c.Links = append(c.Links, map[string]string{"type": l.Type, "target": l.Target})
	}
	return c
}

// Query は要素を検索する。
func Query(p *project.Project, ids []string, typ, status, text string, limit int) []Compact {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []Compact
	for _, id := range p.Order {
		e := p.Elements[id]
		if len(want) > 0 && !want[e.ID] {
			continue
		}
		if typ != "" && e.Type != typ {
			continue
		}
		if status != "" {
			hit := false
			for _, f := range e.Fields {
				if f.Status == status {
					hit = true
				}
			}
			if !hit {
				continue
			}
		}
		if text != "" && score(e, text) == 0 {
			continue
		}
		out = append(out, compact(e))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func score(e *model.Element, text string) int {
	hay := strings.ToLower(e.ID + " " + e.Name)
	for _, f := range e.Fields {
		hay += " " + strings.ToLower(model.Normalize(f.Value))
		for _, c := range f.Claims {
			hay += " " + strings.ToLower(model.Normalize(c.Value))
		}
	}
	n := 0
	for _, w := range strings.Fields(strings.ToLower(text)) {
		if strings.Contains(hay, w) {
			n++
		}
	}
	return n
}

// Pack は context_pack の結果。
type Pack struct {
	Targets        []Compact              `json:"targets"`
	Related        []Compact              `json:"related"`
	StopConditions []string               `json:"stop_conditions"`
	Questions      []gaps.QuestionSummary `json:"open_questions"`
	Drift          []drift.Item           `json:"drift,omitempty"`
	Rules          []string               `json:"rules"`
}

// ContextPack は課題に着手する前の材料をまとめる。
func ContextPack(p *project.Project, ids []string, text string) *Pack {
	g := graph.Build(p)
	pk := &Pack{Rules: []string{
		"Canon の値はすべて出典付き。出典のない推測で補わないこと",
		"stop_conditions に挙がった項目に関わる変更は着手せず、質問の回答を待つこと",
		"変更後は validate で Canon との整合を確かめること",
	}}
	if len(ids) == 0 && text != "" {
		type sc struct {
			id string
			n  int
		}
		var hits []sc
		for _, id := range p.Order {
			if n := score(p.Elements[id], text); n > 0 {
				hits = append(hits, sc{id, n})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
		for i := 0; i < len(hits) && i < 5; i++ {
			ids = append(ids, hits[i].id)
		}
	}
	inTarget := map[string]bool{}
	for _, id := range ids {
		if e, ok := p.Elements[id]; ok {
			pk.Targets = append(pk.Targets, compact(e))
			inTarget[id] = true
		}
	}
	relSeen := map[string]bool{}
	for id := range inTarget {
		for _, r := range g.Related(id) {
			if !inTarget[r] && !relSeen[r] {
				if e, ok := p.Elements[r]; ok {
					relSeen[r] = true
					pk.Related = append(pk.Related, compact(e))
				}
			}
		}
	}
	sort.Slice(pk.Related, func(i, j int) bool { return pk.Related[i].ID < pk.Related[j].ID })
	scope := map[string]bool{}
	for id := range inTarget {
		scope[id] = true
	}
	for id := range relSeen {
		scope[id] = true
	}
	qs := map[string]bool{}
	for _, id := range p.Order {
		if !scope[id] {
			continue
		}
		e := p.Elements[id]
		td := p.Schema.Types[e.Type]
		for _, n := range e.FieldOrder {
			f := e.Fields[n]
			if f.Status != model.StatusUnknown && f.Status != model.StatusConflict {
				continue
			}
			if f.Question != "" {
				qs[f.Question] = true
			}
			if td != nil && td.Fields[n] != nil && td.Fields[n].Blocking {
				st := "未確定"
				if f.Status == model.StatusConflict {
					st = "情報源が矛盾"
				}
				pk.StopConditions = append(pk.StopConditions, fmt.Sprintf("%s.%s（%s）は%s（%s）。この項目に関わる変更は着手しないこと", e.ID, n, td.Fields[n].Label, st, f.Question))
			}
		}
	}
	for id := range qs {
		if q, ok := p.Questions[id]; ok && q.Status == "open" {
			pk.Questions = append(pk.Questions, gaps.QuestionSummary{ID: q.ID, Blocking: q.Blocking, To: q.To, AsksFor: q.AsksFor, Status: q.Status, Context: q.Context})
		}
	}
	sort.Slice(pk.Questions, func(i, j int) bool { return pk.Questions[i].ID < pk.Questions[j].ID })
	for _, it := range drift.Build(p, "", nil).Items {
		if scope[it.Element] {
			pk.Drift = append(pk.Drift, it)
		}
	}
	return pk
}

func strArg(v any) string {
	s, _ := v.(string)
	return s
}

func intArg(v any, def int) int {
	if f, ok := v.(float64); ok && f > 0 {
		return int(f)
	}
	return def
}

func strList(v any) []string {
	var out []string
	if xs, ok := v.([]any); ok {
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
