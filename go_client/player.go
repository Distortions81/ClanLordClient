package main

// Player holds minimal information extracted from BEP messages and descriptors.
type Player struct {
	Name    string
	Race    string
	Gender  string
	Class   string
	Clan    string
	PictID  uint16
	Colors  []byte
	Sharee  bool // player is sharing to us
	Sharing bool // we are sharing to player
}

var players = make(map[string]*Player)

func getPlayer(name string) *Player {
	if p, ok := players[name]; ok {
		return p
	}
	p := &Player{Name: name}
	players[name] = p
	return p
}

func updatePlayerAppearance(name string, pictID uint16, colors []byte) {
	p := getPlayer(name)
	p.PictID = pictID
	if len(colors) > 0 {
		p.Colors = append(p.Colors[:0], colors...)
	}
}
