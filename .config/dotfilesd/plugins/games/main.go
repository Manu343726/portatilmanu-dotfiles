// Games plugin — TUI games (solitaire, minesweeper, 2048, battleship, chess).
//
// Each game runs as a terminal-based interactive session using tview widgets.
package main

import (
	"context"
	"math"
	"math/rand"

	"dotfilesd/plugin"
	pb "plugins/games/proto/games"
	"plugins/games/proto/games/gamesconnect"

	"connectrpc.com/connect"
)

// ─── Minesweeper ────────────────────────────────────────────────────────────

type msGame struct {
	w, h, bombs, flags int
	g                  [][]struct {
		bomb bool
		st   int8
		n    int8
	}
	over, won bool
}

func newMS(w, h, bombs int) *msGame {
	m := &msGame{w: w, h: h, bombs: bombs, flags: bombs}
	m.g = make([][]struct {
		bomb bool
		st   int8
		n    int8
	}, h)
	for y := range m.g {
		m.g[y] = make([]struct {
			bomb bool
			st   int8
			n    int8
		}, w)
	}
	for p := 0; p < bombs; {
		x, y := rand.Intn(w), rand.Intn(h)
		if !m.g[y][x].bomb {
			m.g[y][x].bomb = true
			p++
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if m.g[y][x].bomb {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx >= 0 && nx < w && ny >= 0 && ny < h && m.g[ny][nx].bomb {
						m.g[y][x].n++
					}
				}
			}
		}
	}
	return m
}

func (m *msGame) reveal(x, y int) {
	if x < 0 || x >= m.w || y < 0 || y >= m.h || m.g[y][x].st != 0 {
		return
	}
	m.g[y][x].st = 1
	if m.g[y][x].bomb {
		m.over = true
		return
	}
	if m.g[y][x].n == 0 {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				m.reveal(x+dx, y+dy)
			}
		}
	}
}

func (m *msGame) win() bool {
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			if !m.g[y][x].bomb && m.g[y][x].st != 1 {
				return false
			}
		}
	}
	return true
}

// ─── 2048 ───────────────────────────────────────────────────────────────────

type g2048State struct {
	g      [4][4]int
	sc     int
	ov, wn bool
}

func new2048() *g2048State {
	s := &g2048State{}
	s.sp()
	s.sp()
	return s
}

func (s *g2048State) sp() {
	var cs [][2]int
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if s.g[y][x] == 0 {
				cs = append(cs, [2]int{x, y})
			}
		}
	}
	if len(cs) == 0 {
		return
	}
	c := cs[rand.Intn(len(cs))]
	s.g[c[1]][c[0]] = 2
	if rand.Intn(10) == 0 {
		s.g[c[1]][c[0]] = 4
	}
}

func (s *g2048State) sl(r [4]int) ([4]int, int) {
	var v []int
	for _, x := range r {
		if x != 0 {
			v = append(v, x)
		}
	}
	sc := 0
	for i := 0; i < len(v)-1; i++ {
		if v[i] == v[i+1] {
			v[i] *= 2
			sc += v[i]
			v = append(v[:i+1], v[i+2:]...)
		}
	}
	for len(v) < 4 {
		v = append(v, 0)
	}
	var o [4]int
	copy(o[:], v)
	return o, sc
}

func (s *g2048State) mv(dx, dy int) bool {
	mv := false
	for i := 0; i < 4; i++ {
		var r [4]int
		if dy != 0 {
			for j := 0; j < 4; j++ {
				r[j] = s.g[j][i]
			}
		} else {
			r = s.g[i]
		}
		if dx < 0 || dy < 0 {
			for a, b := 0, 3; a < b; a, b = a+1, b-1 {
				r[a], r[b] = r[b], r[a]
			}
		}
		sl, sc := s.sl(r)
		if dx < 0 || dy < 0 {
			for a, b := 0, 3; a < b; a, b = a+1, b-1 {
				sl[a], sl[b] = sl[b], sl[a]
			}
		}
		if dy != 0 {
			for j := 0; j < 4; j++ {
				if s.g[j][i] != sl[j] {
					mv = true
				}
				s.g[j][i] = sl[j]
			}
		} else {
			if s.g[i] != sl {
				mv = true
			}
			s.g[i] = sl
		}
		s.sc += sc
	}
	return mv
}

func (s *g2048State) can() bool {
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if s.g[y][x] == 0 {
				return true
			}
			if x < 3 && s.g[y][x] == s.g[y][x+1] {
				return true
			}
			if y < 3 && s.g[y][x] == s.g[y+1][x] {
				return true
			}
		}
	}
	return false
}

// ─── Solitaire ──────────────────────────────────────────────────────────────

type card struct {
	s, r int8
	f    bool
}

type solGame struct {
	st, wst []card
	tb      [7][]card
	fd      [4][]card
	ov, wn  bool
}

func deck() []card {
	d := make([]card, 52)
	i := 0
	for s := int8(0); s < 4; s++ {
		for r := int8(1); r <= 13; r++ {
			d[i] = card{s, r, false}
			i++
		}
	}
	rand.Shuffle(52, func(i, j int) { d[i], d[j] = d[j], d[i] })
	return d
}

func newSol() *solGame {
	g := &solGame{}
	d := deck()
	idx := 0
	for i := 0; i < 7; i++ {
		for j := i; j < 7; j++ {
			c := d[idx]
			if j == i {
				c.f = true
			}
			g.tb[j] = append(g.tb[j], c)
			idx++
		}
	}
	g.st = d[idx:]
	return g
}

func (g *solGame) tp(c int) *card {
	if len(g.tb[c]) == 0 {
		return nil
	}
	return &g.tb[c][len(g.tb[c])-1]
}
func (g *solGame) ft(i int) *card {
	if len(g.fd[i]) == 0 {
		return nil
	}
	return &g.fd[i][len(g.fd[i])-1]
}
func (g *solGame) face(c int) {
	if len(g.tb[c]) > 0 {
		g.tb[c][len(g.tb[c])-1].f = true
	}
}
func (g *solGame) can(c card, col int) bool {
	t := g.tp(col)
	if t == nil {
		return c.r == 13
	}
	return t.f && (c.s/2 != t.s/2) && c.r == t.r-1
}
func (g *solGame) fdx(c card) int {
	for i := 0; i < 4; i++ {
		t := g.ft(i)
		if t == nil {
			if c.r == 1 {
				return i
			}
			continue
		}
		if t.s == c.s && t.r == c.r-1 {
			return i
		}
	}
	return -1
}

func (g *solGame) draw() {
	if len(g.st) == 0 {
		g.st = g.wst
		g.wst = nil
		for i := len(g.st)/2 - 1; i >= 0; i-- {
			o := len(g.st) - 1 - i
			g.st[i], g.st[o] = g.st[o], g.st[i]
		}
		return
	}
	c := g.st[len(g.st)-1]
	g.st = g.st[:len(g.st)-1]
	c.f = true
	g.wst = append(g.wst, c)
}

func (g *solGame) auto() {
	for mv := true; mv; {
		mv = false
		for i := 0; i < 7; i++ {
			c := g.tp(i)
			if c == nil || !c.f {
				continue
			}
			if idx := g.fdx(*c); idx >= 0 {
				g.fd[idx] = append(g.fd[idx], g.tb[i][len(g.tb[i])-1])
				g.tb[i] = g.tb[i][:len(g.tb[i])-1]
				g.face(i)
				mv = true
			}
		}
	}
}

func (g *solGame) move(c, d, n int) bool {
	if c == d || c < 0 || c > 6 || d < 0 || d > 6 || n < 1 || n > len(g.tb[c]) {
		return false
	}
	i := len(g.tb[c]) - n
	if !g.tb[c][i].f || !g.can(g.tb[c][i], d) {
		return false
	}
	g.tb[d] = append(g.tb[d], g.tb[c][i:]...)
	g.tb[c] = g.tb[c][:i]
	g.face(c)
	return true
}

// ─── Battleship ────────────────────────────────────────────────────────────

type bsState struct {
	pg, ag     [10][10]int8
	ph         int
	ov, wn     bool
}

func newBS() *bsState { s := &bsState{}; s.placeAI(); return s }

func (s *bsState) can(g *[10][10]int8, x, y, sz int, h bool) bool {
	for i := 0; i < sz; i++ {
		cx, cy := x, y
		if h {
			cx += i
		} else {
			cy += i
		}
		if cx >= 10 || cy >= 10 || g[cy][cx] != 0 {
			return false
		}
	}
	return true
}
func (s *bsState) p(g *[10][10]int8, x, y, sz int, h bool) {
	for i := 0; i < sz; i++ {
		cx, cy := x, y
		if h {
			cx += i
		} else {
			cy += i
		}
		g[cy][cx] = 1
	}
}
func (s *bsState) sunk(g *[10][10]int8) bool {
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if g[y][x] == 1 {
				return false
			}
		}
	}
	return true
}
func (s *bsState) placeAI() {
	sz := []int{5, 4, 3, 3, 2}
	for _, sz := range sz {
		for {
			x, y := rand.Intn(10), rand.Intn(10)
			h := rand.Intn(2) == 0
			if s.can(&s.ag, x, y, sz, h) {
				s.p(&s.ag, x, y, sz, h)
				break
			}
		}
	}
}

// ─── Chess ──────────────────────────────────────────────────────────────────

type chState struct {
	b      [8][8]int8
	t      int8
	ov, wn bool
	wi     int8
	ep     [2]int8
	ck, cq [2]bool
}

const (
	_ = iota
	cP
	cN
	cB
	cR
	cQ
	cK
)

func newCh() *chState {
	g := &chState{t: 1, ck: [2]bool{true, true}, cq: [2]bool{true, true}, ep: [2]int8{-1, -1}}
	back := [8]int8{cR, cN, cB, cQ, cK, cB, cN, cR}
	for x := 0; x < 8; x++ {
		g.b[0][x] = -back[x]
		g.b[1][x] = -cP
		g.b[6][x] = cP
		g.b[7][x] = back[x]
	}
	return g
}

func (g *chState) ib(x, y int8) bool { return x >= 0 && x < 8 && y >= 0 && y < 8 }
func (g *chState) ab(v int8) int8 {
	if v < 0 {
		return -v
	}
	return v
}

func (g *chState) at(x, y int8, by int8) bool {
	for sy := 0; sy < 8; sy++ {
		for sx := 0; sx < 8; sx++ {
			p := g.b[sy][sx]
			if p == 0 || p/g.ab(p) != by {
				continue
			}
			dx, dy := int(x)-sx, int(y)-sy
			adx, ady := absI(dx), absI(dy)
			switch g.ab(p) {
			case cP:
				if (int(by) == 1 && dy == -1 || int(by) == -1 && dy == 1) && adx == 1 {
					return true
				}
			case cN:
				if (adx == 2 && ady == 1) || (adx == 1 && ady == 2) {
					return true
				}
			case cB:
				if adx == ady && adx > 0 {
					bl := false
					for i := 1; i < adx; i++ {
						if g.b[sy+sD(dy)*i][sx+sD(dx)*i] != 0 {
							bl = true
							break
						}
					}
					if !bl {
						return true
					}
				}
			case cR:
				if dx == 0 && dy != 0 {
					bl := false
					for i := 1; i < ady; i++ {
						if g.b[sy+sD(dy)*i][sx] != 0 {
							bl = true
							break
						}
					}
					if !bl {
						return true
					}
				}
				if dy == 0 && dx != 0 {
					bl := false
					for i := 1; i < adx; i++ {
						if g.b[sy][sx+sD(dx)*i] != 0 {
							bl = true
							break
						}
					}
					if !bl {
						return true
					}
				}
			case cQ:
				if adx == ady && adx > 0 {
					bl := false
					for i := 1; i < adx; i++ {
						if g.b[sy+sD(dy)*i][sx+sD(dx)*i] != 0 {
							bl = true
							break
						}
					}
					if !bl {
						return true
					}
				}
				if dx == 0 && dy != 0 {
					bl := false
					for i := 1; i < ady; i++ {
						if g.b[sy+sD(dy)*i][sx] != 0 {
							bl = true
							break
						}
					}
					if !bl {
						return true
					}
				}
				if dy == 0 && dx != 0 {
					bl := false
					for i := 1; i < adx; i++ {
						if g.b[sy][sx+sD(dx)*i] != 0 {
							bl = true
							break
						}
					}
					if !bl {
						return true
					}
				}
			case cK:
				if adx <= 1 && ady <= 1 {
					return true
				}
			}
		}
	}
	return false
}

func absI(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func sD(d int) int {
	if d < 0 {
		return -1
	}
	return 1
}

func (g *chState) ic(c int8) bool {
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if g.b[y][x] == c*cK {
				return g.at(int8(x), int8(y), -c)
			}
		}
	}
	return false
}

func (g *chState) lg(x1, y1, x2, y2 int8) bool {
	p := g.b[y1][x1]
	if p == 0 || p/g.ab(p) != g.t || !g.ib(x2, y2) {
		return false
	}
	t := g.b[y2][x2]
	if t != 0 && t/g.ab(t) == g.t {
		return false
	}
	dx, dy := int(x2-x1), int(y2-y1)
	pt := g.ab(p)
	adx, ady := absI(dx), absI(dy)
	v := false
	iy1, ix1 := int(y1), int(x1)
	switch pt {
	case cP:
		if g.t == 1 && dy == -1 && dx == 0 && t == 0 || g.t == -1 && dy == 1 && dx == 0 && t == 0 ||
			g.t == 1 && dy == -2 && dx == 0 && iy1 == 6 && t == 0 && g.b[5][x1] == 0 || g.t == -1 && dy == 2 && dx == 0 && iy1 == 1 && t == 0 && g.b[2][x1] == 0 {
			v = true
		}
		if adx == 1 && ((g.t == 1 && dy == -1) || (g.t == -1 && dy == 1)) && ((t != 0 && t/g.ab(t) != g.t) || (t == 0 && x2 == g.ep[0] && y2 == g.ep[1])) {
			v = true
		}
	case cN:
		if (adx == 2 && ady == 1) || (adx == 1 && ady == 2) {
			v = true
		}
	case cB:
		if adx == ady && adx > 0 {
			bl := false
			for i := 1; i < adx; i++ {
				if g.b[iy1+sD(dy)*i][ix1+sD(dx)*i] != 0 {
					bl = true
					break
				}
			}
			v = !bl
		}
	case cR:
		if dx == 0 && dy != 0 {
			bl := false
			for i := 1; i < ady; i++ {
				if g.b[iy1+sD(dy)*i][ix1] != 0 {
					bl = true
					break
				}
			}
			v = !bl
		}
		if dy == 0 && dx != 0 {
			bl := false
			for i := 1; i < adx; i++ {
				if g.b[iy1][ix1+sD(dx)*i] != 0 {
					bl = true
					break
				}
			}
			v = !bl
		}
	case cQ:
		if adx == ady && adx > 0 {
			bl := false
			for i := 1; i < adx; i++ {
				if g.b[iy1+sD(dy)*i][ix1+sD(dx)*i] != 0 {
					bl = true
					break
				}
			}
			v = !bl
		}
		if dx == 0 && dy != 0 {
			bl := false
			for i := 1; i < ady; i++ {
				if g.b[iy1+sD(dy)*i][ix1] != 0 {
					bl = true
					break
				}
			}
			v = !bl
		}
		if dy == 0 && dx != 0 {
			bl := false
			for i := 1; i < adx; i++ {
				if g.b[iy1][ix1+sD(dx)*i] != 0 {
					bl = true
					break
				}
			}
			v = !bl
		}
	case cK:
		if adx <= 1 && ady <= 1 {
			v = true
		}
		if adx == 2 && dy == 0 {
			co := (g.t + 1) / 2
			if y1 == 7 && g.t == 1 || y1 == 0 && g.t == -1 {
				if dx == 2 && g.ck[co] && g.b[y1][5] == 0 && g.b[y1][6] == 0 && !g.at(4, y1, -g.t) && !g.at(5, y1, -g.t) && !g.at(6, y1, -g.t) {
					v = true
				}
				if dx == -2 && g.cq[co] && g.b[y1][3] == 0 && g.b[y1][2] == 0 && g.b[y1][1] == 0 && !g.at(4, y1, -g.t) && !g.at(3, y1, -g.t) && !g.at(2, y1, -g.t) {
					v = true
				}
			}
		}
	}
	if !v {
		return false
	}
	sv := g.b[y2][x2]
	g.b[y2][x2] = p
	g.b[y1][x1] = 0
	ic := g.ic(g.t)
	g.b[y1][x1] = p
	g.b[y2][x2] = sv
	return !ic
}

func (g *chState) hm(c int8) bool {
	for y1 := 0; y1 < 8; y1++ {
		for x1 := 0; x1 < 8; x1++ {
			if g.b[y1][x1] == 0 || g.b[y1][x1]/g.ab(g.b[y1][x1]) != c {
				continue
			}
			for y2 := 0; y2 < 8; y2++ {
				for x2 := 0; x2 < 8; x2++ {
					if g.lg(int8(x1), int8(y1), int8(x2), int8(y2)) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (g *chState) ap(x1, y1, x2, y2 int8) {
	p := g.b[y1][x1]
	if g.ab(p) == cK && g.ab(x2-x1) == 2 {
		if x2 > x1 {
			g.b[y1][5], g.b[y1][7] = g.b[y1][7], 0
		} else {
			g.b[y1][3], g.b[y1][0] = g.b[y1][0], 0
		}
	}
	if g.ab(p) == cP && x2 == g.ep[0] && y2 == g.ep[1] {
		g.b[y1][x2] = 0
	}
	if g.ab(p) == cP && (y2 == 0 || y2 == 7) {
		p = g.t * cQ
	}
	g.b[y2][x2] = p
	g.b[y1][x1] = 0
	g.ep = [2]int8{-1, -1}
	if g.ab(p) == cP && g.ab(y2-y1) == 2 {
		g.ep = [2]int8{x1, (y1 + y2) / 2}
	}
	co := (g.t + 1) / 2
	if g.ab(p) == cK {
		g.ck[co], g.cq[co] = false, false
	}
	if x1 == 0 && y1 == 7 {
		g.cq[0] = false
	}
	if x1 == 7 && y1 == 7 {
		g.ck[0] = false
	}
	if x1 == 0 && y1 == 0 {
		g.cq[1] = false
	}
	if x1 == 7 && y1 == 0 {
		g.ck[1] = false
	}
	if x2 == 0 && y2 == 7 {
		g.cq[0] = false
	}
	if x2 == 7 && y2 == 7 {
		g.ck[0] = false
	}
	if x2 == 0 && y2 == 0 {
		g.cq[1] = false
	}
	if x2 == 7 && y2 == 0 {
		g.ck[1] = false
	}
	g.t = -g.t
	if !g.hm(g.t) {
		if g.ic(g.t) {
			g.wn, g.wi = true, -g.t
		}
		g.ov = true
	}
}

func (g *chState) ai() {
	vl := map[int8]int{cP: 100, cN: 320, cB: 330, cR: 500, cQ: 900, cK: 20000}
	bs := math.MinInt32
	var bx, by, bx2, by2 int8
	for y1 := 0; y1 < 8; y1++ {
		for x1 := 0; x1 < 8; x1++ {
			p := g.b[y1][x1]
			if p == 0 || p/g.ab(p) != g.t {
				continue
			}
			for y2 := 0; y2 < 8; y2++ {
				for x2 := 0; x2 < 8; x2++ {
					if !g.lg(int8(x1), int8(y1), int8(x2), int8(y2)) {
						continue
					}
					sc := 0
					if t := g.b[y2][x2]; t != 0 {
						sc += vl[g.ab(t)]
					}
					if g.ab(p) == cP && (y2 == 0 || y2 == 7) {
						sc += 700
					}
					sc += rand.Intn(20)
					if sc > bs {
						bs, bx, by, bx2, by2 = sc, int8(x1), int8(y1), int8(x2), int8(y2)
					}
				}
			}
		}
	}
	if bs > math.MinInt32 {
		g.ap(bx, by, bx2, by2)
	}
}

// ─── service implementations ────────────────────────────────────────────────

type msSvc struct{}

func (s *msSvc) Play(ctx context.Context, req *connect.Request[pb.MinesweeperRequest]) (*connect.Response[pb.PlayResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return r("no context")
	}
	w, h, b := int(req.Msg.Width), int(req.Msg.Height), int(req.Msg.Bombs)
	if w <= 0 {
		w = 9
	}
	if h <= 0 {
		h = 9
	}
	if b <= 0 {
		b = 10
	}
	if b >= w*h {
		b = w*h - 1
	}
	won := runMinesweeper(pc, w, h, b)
	return r(map[bool]string{true: "You won minesweeper!", false: "Boom!"}[won])
}

type tSvc struct{}

func (s *tSvc) Play(ctx context.Context, req *connect.Request[pb.PlayRequest]) (*connect.Response[pb.PlayResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return r("no context")
	}
	won := run2048(pc)
	return r(map[bool]string{true: "2048 reached!", false: "Game over"}[won])
}

type solSvc struct{}

func (s *solSvc) Play(ctx context.Context, req *connect.Request[pb.PlayRequest]) (*connect.Response[pb.PlayResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return r("no context")
	}
	won := runSolitaire(pc)
	return r(map[bool]string{true: "Solitaire completed!", false: "Game over"}[won])
}

type bsSvc struct{}

func (s *bsSvc) Play(ctx context.Context, req *connect.Request[pb.PlayRequest]) (*connect.Response[pb.PlayResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return r("no context")
	}
	won := runBattleship(pc)
	return r(map[bool]string{true: "All ships sunk!", false: "Fleet destroyed"}[won])
}

type chSvc struct{}

func (s *chSvc) Play(ctx context.Context, req *connect.Request[pb.PlayRequest]) (*connect.Response[pb.PlayResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return r("no context")
	}
	won := runChess(pc)
	return r(map[bool]string{true: "Checkmate!", false: "AI wins"}[won])
}

func r(s string) (*connect.Response[pb.PlayResponse], error) {
	return connect.NewResponse(&pb.PlayResponse{Summary: s}), nil
}

// ─── main ──────────────────────────────────────────────────────────────────

func main() {
	msP, msH := gamesconnect.NewMinesweeperServiceHandler(&msSvc{})
	tP, tH := gamesconnect.NewGame2048ServiceHandler(&tSvc{})
	sP, sH := gamesconnect.NewSolitaireServiceHandler(&solSvc{})
	bP, bH := gamesconnect.NewBattleshipServiceHandler(&bsSvc{})
	cP, cH := gamesconnect.NewChessServiceHandler(&chSvc{})

	plugin.Serve(plugin.Config{
		Name: "games", DisplayName: "Games", Version: "1.0.0",
		Description: "TUI games: minesweeper, 2048, solitaire, battleship, chess",
		DocsProto:   pb.PluginDocs,
		Services: []plugin.Service{
			{Name: "games.MinesweeperService", Description: "Minesweeper", Path: msP, Handler: msH, PluginAccessible: true, InteractiveMethods: []string{"Play"}},
			{Name: "games.Game2048Service", Description: "2048", Path: tP, Handler: tH, PluginAccessible: true, InteractiveMethods: []string{"Play"}},
			{Name: "games.SolitaireService", Description: "Klondike solitaire", Path: sP, Handler: sH, PluginAccessible: true, InteractiveMethods: []string{"Play"}},
			{Name: "games.BattleshipService", Description: "Battleship vs AI", Path: bP, Handler: bH, PluginAccessible: true, InteractiveMethods: []string{"Play"}},
			{Name: "games.ChessService", Description: "Chess vs AI", Path: cP, Handler: cH, PluginAccessible: true, InteractiveMethods: []string{"Play"}},
		},
	})
}
