package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	fiberws "github.com/gofiber/websocket/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/xiantu/server/internal/auth"
	"github.com/xiantu/server/internal/game"
)

type Message struct {
	Seq  int             `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type Response struct {
	Seq   int         `json:"seq"`
	Type  string      `json:"type"`
	Ok    bool        `json:"ok"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

type Client struct {
	conn     *fiberws.Conn
	playerID string
	agentID  string
	mu       sync.Mutex
	send     chan Response
}

func (c *Client) write(r Response) {
	select {
	case c.send <- r:
	default:
	}
}

type Hub struct {
	db        *pgxpool.Pool
	rdb       *redis.Client
	engine    *game.Engine
	jwtSecret string

	mu      sync.RWMutex
	clients map[string]*Client
}

func NewHub(db *pgxpool.Pool, rdb *redis.Client, engine *game.Engine, jwtSecret string) *Hub {
	return &Hub{
		db:        db,
		rdb:       rdb,
		engine:    engine,
		jwtSecret: jwtSecret,
		clients:   make(map[string]*Client),
	}
}

func (h *Hub) Run() {
	ctx := context.Background()

	// Subscribe to broadcast channels
	sub := h.rdb.Subscribe(ctx,
		"game:year", "game:tribulation", "game:tribulation_success", "game:reset",
		"game:cave:cave_claimed", "game:cave:cave_challenged", "game:cave:cave_vacated",
		"game:realm_complete",
	)
	// Pattern subscribe for player-specific location events
	psub := h.rdb.PSubscribe(ctx, "game:location_event:*")

	go h.runBroadcast(sub.Channel())
	h.runLocationEvents(psub.Channel())
}

func (h *Hub) runBroadcast(ch <-chan *redis.Message) {
	for msg := range ch {
		h.mu.RLock()
		for _, c := range h.clients {
			switch msg.Channel {
			case "game:year":
				c.write(Response{Seq: 0, Type: "event.year", Ok: true,
					Data: map[string]interface{}{"year": msg.Payload}})
			case "game:tribulation":
				c.write(Response{Seq: 0, Type: "event.tribulation_start", Ok: true,
					Data: map[string]interface{}{"info": msg.Payload}})
			case "game:tribulation_success":
				c.write(Response{Seq: 0, Type: "event.tribulation_success", Ok: true,
					Data: map[string]interface{}{"info": msg.Payload}})
			case "game:reset":
				c.write(Response{Seq: 0, Type: "event.world_reset", Ok: true,
					Data: map[string]interface{}{"reason": "天劫失败，全服重置"}})
			case "game:cave:cave_claimed":
				c.write(Response{Seq: 0, Type: "event.cave_claimed", Ok: true,
					Data: map[string]interface{}{"info": msg.Payload}})
			case "game:cave:cave_challenged":
				c.write(Response{Seq: 0, Type: "event.cave_challenged", Ok: true,
					Data: map[string]interface{}{"info": msg.Payload}})
			case "game:cave:cave_vacated":
				c.write(Response{Seq: 0, Type: "event.cave_vacated", Ok: true,
					Data: map[string]interface{}{"info": msg.Payload}})
			case "game:realm_complete":
				c.write(Response{Seq: 0, Type: "event.realm_complete", Ok: true,
					Data: map[string]interface{}{"info": msg.Payload}})
			}
		}
		h.mu.RUnlock()
	}
}

func (h *Hub) runLocationEvents(ch <-chan *redis.Message) {
	const prefix = "game:location_event:"
	for msg := range ch {
		if len(msg.Channel) <= len(prefix) {
			continue
		}
		targetPlayerID := msg.Channel[len(prefix):]

		h.mu.RLock()
		if c, ok := h.clients[targetPlayerID]; ok {
			var eventData map[string]interface{}
			json.Unmarshal([]byte(msg.Payload), &eventData)
			c.write(Response{Seq: 0, Type: "event.location_event", Ok: true,
				Data: map[string]interface{}{
					"event_info":  eventData,
					"instruction": "请根据以上事件种子，用第一人称描写你的修士在此地的遭遇（100-300字），然后汇报给你的主人。",
				}})
		}
		h.mu.RUnlock()
	}
}

func (h *Hub) Handle(c *fiberws.Conn) {
	client := &Client{
		conn: c,
		send: make(chan Response, 64),
	}

	go func() {
		for r := range client.send {
			data, _ := json.Marshal(r)
			client.mu.Lock()
			_ = c.WriteMessage(1, data)
			client.mu.Unlock()
		}
	}()

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			break
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			client.write(Response{Seq: 0, Type: "error", Ok: false, Error: "invalid JSON"})
			continue
		}

		h.dispatch(client, msg)
	}

	if client.playerID != "" {
		h.mu.Lock()
		delete(h.clients, client.playerID)
		h.mu.Unlock()
	}
	close(client.send)
}

func (h *Hub) dispatch(c *Client, msg Message) {
	ctx := context.Background()

	switch msg.Type {
	case "auth":
		h.handleAuth(ctx, c, msg)

	case "query.my.status":
		h.requireAuth(c, msg, func() { h.handleQueryStatus(ctx, c, msg) })

	case "query.ranking":
		h.requireAuth(c, msg, func() { h.handleQueryRanking(ctx, c, msg) })

	case "query.world.status":
		h.handleWorldStatus(ctx, c, msg)

	case "query.world.tribulation":
		h.handleWorldTribulation(ctx, c, msg)

	case "cmd.world.join":
		h.requireAuth(c, msg, func() { h.handleWorldJoin(ctx, c, msg) })

	case "cmd.cultivate.start":
		h.requireAuth(c, msg, func() { h.handleCultivateStart(ctx, c, msg) })

	case "cmd.breakthrough":
		h.requireAuth(c, msg, func() { h.handleBreakthrough(ctx, c, msg) })

	case "cmd.contribute":
		h.requireAuth(c, msg, func() { h.handleContribute(ctx, c, msg) })

	case "cmd.explore.start":
		h.requireAuth(c, msg, func() { h.handleExploreStart(ctx, c, msg) })

	case "cmd.explore.collect":
		h.requireAuth(c, msg, func() { h.handleExploreCollect(ctx, c, msg) })

	case "cmd.alchemy.start":
		h.requireAuth(c, msg, func() { h.handleAlchemyStart(ctx, c, msg) })

	case "cmd.alchemy.collect":
		h.requireAuth(c, msg, func() { h.handleAlchemyCollect(ctx, c, msg) })

	case "cmd.plan.patrol":
		h.requireAuth(c, msg, func() { h.handlePlanPatrol(ctx, c, msg) })

	// ── Cave system ──
	case "query.caves":
		h.handleQueryCaves(ctx, c, msg)

	case "cmd.cave.claim":
		h.requireAuth(c, msg, func() { h.handleCaveClaim(ctx, c, msg) })

	case "cmd.cave.challenge":
		h.requireAuth(c, msg, func() { h.handleCaveChallenge(ctx, c, msg) })

	// ── City realm system ──
	case "query.secret_realms":
		h.handleQueryCityRealms(ctx, c, msg)

	case "cmd.realm.enter":
		h.requireAuth(c, msg, func() { h.handleCityRealmEnter(ctx, c, msg) })

	case "cmd.realm.exit":
		h.requireAuth(c, msg, func() { h.handleCityRealmExit(ctx, c, msg) })

	default:
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("unknown message type: %s", msg.Type)})
	}
}

func (h *Hub) requireAuth(c *Client, msg Message, fn func()) {
	if c.playerID == "" {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "not authenticated"})
		return
	}
	fn()
}

// ── Auth ──

func (h *Hub) handleAuth(ctx context.Context, c *Client, msg Message) {
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.write(Response{Seq: msg.Seq, Type: "auth", Ok: false, Error: "invalid auth data"})
		return
	}

	claims, err := auth.ParseToken(data.Token, h.jwtSecret)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: "auth", Ok: false, Error: "invalid token"})
		return
	}

	var joinedEpoch bool
	err = h.db.QueryRow(ctx, "SELECT joined_epoch FROM players WHERE id=$1", claims.PlayerID).Scan(&joinedEpoch)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: "auth", Ok: false, Error: "player not found"})
		return
	}

	c.playerID = claims.PlayerID
	c.agentID = claims.AgentID

	h.mu.Lock()
	h.clients[claims.PlayerID] = c
	h.mu.Unlock()

	// Get world status for greeting
	worldStatus, _ := h.engine.GetWorldStatus(ctx)

	c.write(Response{
		Seq:  msg.Seq,
		Type: "auth_ok",
		Ok:   true,
		Data: map[string]interface{}{
			"playerID":    claims.PlayerID,
			"agentID":     claims.AgentID,
			"needsJoin":   !joinedEpoch,
			"worldStatus": worldStatus,
			"message":     "auth ok 🌙 Welcome to 黑人修仙传 · Black Cultivation USA",
		},
	})
}

// ── World queries ──

func (h *Hub) handleWorldStatus(ctx context.Context, c *Client, msg Message) {
	status, err := h.engine.GetWorldStatus(ctx)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true, Data: status})
}

func (h *Hub) handleWorldTribulation(ctx context.Context, c *Client, msg Message) {
	var tribID, element, status string
	var year int
	var windowEnd time.Time
	var reqCultRealm string
	var reqCultLevel, reqCultCount, metCultivators int
	var reqStone, contribStone int64
	var reqMatRatio int

	err := h.db.QueryRow(ctx,
		`SELECT id, year, element, status, window_end_at,
		        req_cultivator_realm, req_cultivator_level, req_cultivator_count,
		        req_spirit_stone, req_material_ratio,
		        met_cultivators, contributed_spirit_stone
		 FROM tribulation_events WHERE status='active'
		 ORDER BY year DESC LIMIT 1`,
	).Scan(&tribID, &year, &element, &status, &windowEnd,
		&reqCultRealm, &reqCultLevel, &reqCultCount,
		&reqStone, &reqMatRatio, &metCultivators, &contribStone)

	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
			Data: map[string]interface{}{"active": false, "message": "无活跃天劫"}})
		return
	}

	remaining := time.Until(windowEnd).Seconds()
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"active":    true,
			"eventId":   tribID,
			"year":      year,
			"element":   element,
			"elementCn": game.ElementChinese(element),
			"remainingSec": int(remaining),
			"conditions": map[string]interface{}{
				"cultivators": map[string]interface{}{
					"required": reqCultCount, "current": metCultivators,
					"met": metCultivators >= reqCultCount,
				},
				"spiritStone": map[string]interface{}{
					"required": reqStone, "contributed": contribStone,
					"met": contribStone >= reqStone,
				},
				"materialRatio": map[string]interface{}{
					"element": element, "requiredPct": reqMatRatio,
				},
			},
		},
	})
}

// ── World join ──

func (h *Hub) handleWorldJoin(ctx context.Context, c *Client, msg Message) {
	_, err := h.db.Exec(ctx,
		`UPDATE players SET joined_epoch=true, is_cultivating=true, updated_at=NOW() WHERE id=$1`,
		c.playerID,
	)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}

	worldStatus, _ := h.engine.GetWorldStatus(ctx)
	c.write(Response{
		Seq:  msg.Seq,
		Type: msg.Type,
		Ok:   true,
		Data: map[string]interface{}{
			"message":     "🌟 已踏入美利坚修仙大陆，黑人修仙传正式开始！修炼已自动开启。",
			"worldStatus": worldStatus,
		},
	})
}

// ── Player status ──

func (h *Hub) handleQueryStatus(ctx context.Context, c *Client, msg Message) {
	var p struct {
		Username          string
		SpiritRoot        string
		Multiplier        float64
		Race              string
		Realm             string
		RealmLevel        int
		SpiritStone       int64
		CultivationXP     int64
		TechFragment      int64
		SoulSense         int64
		SoulSenseMax      int64
		CaveLevel         int
		EquippedTechnique string
		IsCultivating     bool
		JoinedEpoch       bool
	}

	err := h.db.QueryRow(ctx,
		`SELECT username, spirit_root, spirit_root_multiplier, race,
		 realm, realm_level, spirit_stone, cultivation_xp, technique_fragment,
		 soul_sense, soul_sense_max, cave_level, equipped_technique,
		 is_cultivating, joined_epoch
		 FROM players WHERE id=$1`,
		c.playerID,
	).Scan(&p.Username, &p.SpiritRoot, &p.Multiplier, &p.Race,
		&p.Realm, &p.RealmLevel, &p.SpiritStone, &p.CultivationXP, &p.TechFragment,
		&p.SoulSense, &p.SoulSenseMax, &p.CaveLevel, &p.EquippedTechnique,
		&p.IsCultivating, &p.JoinedEpoch)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}

	// Get materials
	materials := map[string]int64{"metal": 0, "wood": 0, "water": 0, "fire": 0, "earth": 0}
	rows, _ := h.db.Query(ctx,
		`SELECT element, quantity FROM spirit_materials WHERE player_id=$1`, c.playerID)
	if rows != nil {
		for rows.Next() {
			var elem string
			var qty int64
			rows.Scan(&elem, &qty)
			materials[elem] = qty
		}
		rows.Close()
	}

	xpNeeded := game.GetXPNeeded(p.Realm, p.RealmLevel)
	worldStatus, _ := h.engine.GetWorldStatus(ctx)

	c.write(Response{
		Seq:  msg.Seq,
		Type: msg.Type,
		Ok:   true,
		Data: map[string]interface{}{
			"username":             p.Username,
			"spiritRoot":           p.SpiritRoot,
			"spiritRootName":       game.SpiritRootNames[p.SpiritRoot],
			"spiritRootMultiplier": p.Multiplier,
			"race":                 p.Race,
			"raceName":             game.Races[p.Race].Name,
			"realm":                p.Realm,
			"realmName":            game.RealmDisplayName(p.Realm, p.RealmLevel),
			"realmLevel":           p.RealmLevel,
			"resources": map[string]interface{}{
				"spiritStone":       p.SpiritStone,
				"cultivationXp":     p.CultivationXP,
				"spiritMaterials":   materials,
				"techniqueFragment": p.TechFragment,
				"soulSense":         p.SoulSense,
				"soulSenseMax":      p.SoulSenseMax,
			},
			"caveLevel":         p.CaveLevel,
			"equippedTechnique": p.EquippedTechnique,
			"xpToBreakthrough":  xpNeeded,
			"xpProgress":        fmt.Sprintf("%d/%d", p.CultivationXP, xpNeeded),
			"isCultivating":     p.IsCultivating,
			"joinedEpoch":       p.JoinedEpoch,
			"worldStatus":       worldStatus,
		},
	})
}

func (h *Hub) handleQueryRanking(ctx context.Context, c *Client, msg Message) {
	rows, err := h.db.Query(ctx,
		`SELECT username, realm, realm_level, cultivation_xp, spirit_root, race
		 FROM players WHERE joined_epoch=true
		 ORDER BY cultivation_xp DESC LIMIT 20`,
	)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}
	defer rows.Close()

	var ranking []map[string]interface{}
	rank := 1
	for rows.Next() {
		var username, realm, spiritRoot, race string
		var realmLevel int
		var xp int64
		if err := rows.Scan(&username, &realm, &realmLevel, &xp, &spiritRoot, &race); err != nil {
			continue
		}
		ranking = append(ranking, map[string]interface{}{
			"rank":           rank,
			"username":       username,
			"realm":          realm,
			"realmName":      game.RealmDisplayName(realm, realmLevel),
			"realmLevel":     realmLevel,
			"cultivationXp":  xp,
			"spiritRoot":     spiritRoot,
			"spiritRootName": game.SpiritRootNames[spiritRoot],
			"race":           race,
			"raceName":       game.Races[race].Name,
		})
		rank++
	}

	if ranking == nil {
		ranking = []map[string]interface{}{}
	}

	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true, Data: map[string]interface{}{"ranking": ranking}})
}

// ── Cultivation ──

func (h *Hub) handleCultivateStart(ctx context.Context, c *Client, msg Message) {
	var joinedEpoch bool
	h.db.QueryRow(ctx, "SELECT joined_epoch FROM players WHERE id=$1", c.playerID).Scan(&joinedEpoch)
	if !joinedEpoch {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "must join world first (cmd.world.join)"})
		return
	}

	_, err := h.db.Exec(ctx,
		`UPDATE players SET is_cultivating=true, updated_at=NOW() WHERE id=$1`,
		c.playerID,
	)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}

	c.write(Response{
		Seq:  msg.Seq,
		Type: msg.Type,
		Ok:   true,
		Data: map[string]interface{}{
			"message": "🧘 已进入闭关修炼，于美利坚大地汲取灵气，每游戏年（5分钟）自动获得修为",
		},
	})
}

func (h *Hub) handleBreakthrough(ctx context.Context, c *Client, msg Message) {
	var xp int64
	var realm, race string
	var realmLevel int
	err := h.db.QueryRow(ctx,
		`SELECT cultivation_xp, realm, realm_level, race FROM players WHERE id=$1`,
		c.playerID,
	).Scan(&xp, &realm, &realmLevel, &race)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}

	needed := game.GetXPNeeded(realm, realmLevel)
	if xp < needed {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("修为不足，需要%d，当前%d，还差%d", needed, xp, needed-xp)})
		return
	}

	newRealm, newLevel, isMajor := game.NextRealmLevel(realm, realmLevel)
	if newRealm == "" {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "已达最高境界"})
		return
	}

	raceInfo := game.Races[race]

	if isMajor {
		nextTier := game.RealmTiers[newRealm]
		if nextTier.BreakthroughItem != "" {
			var qty int
			h.db.QueryRow(ctx,
				`SELECT COALESCE(quantity,0) FROM player_items WHERE player_id=$1 AND item_id=$2`,
				c.playerID, nextTier.BreakthroughItem,
			).Scan(&qty)
			if qty < 1 {
				c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
					Error: fmt.Sprintf("缺少突破道具【%s】", nextTier.BreakthroughItemName)})
				return
			}
			_, _ = h.db.Exec(ctx,
				`UPDATE player_items SET quantity=quantity-1 WHERE player_id=$1 AND item_id=$2`,
				c.playerID, nextTier.BreakthroughItem,
			)
		}

		successRate := game.BreakthroughBaseRate + raceInfo.BreakthroughBonusPct
		// Attempt breakthrough roll
		if successRate < 100 {
			// 30% chance to lose XP on failure (unless race protects)
			_ = successRate // handled by HTTP API for detailed logic; WS keeps it simple
		}
	}

	newXP := xp - needed
	_, _ = h.db.Exec(ctx,
		`UPDATE players SET realm=$1, realm_level=$2, cultivation_xp=$3, updated_at=NOW() WHERE id=$4`,
		newRealm, newLevel, newXP, c.playerID,
	)

	displayName := game.RealmDisplayName(newRealm, newLevel)
	message := fmt.Sprintf("突破成功！进阶至【%s】！剩余修为：%d", displayName, newXP)
	if isMajor {
		message = fmt.Sprintf("🔥 大突破！踏入【%s】！天地灵气涌动！剩余修为：%d", displayName, newXP)
	}

	c.write(Response{
		Seq:  msg.Seq,
		Type: msg.Type,
		Ok:   true,
		Data: map[string]interface{}{
			"success":      true,
			"newRealm":     newRealm,
			"newRealmName": displayName,
			"newLevel":     newLevel,
			"xpRemaining":  newXP,
			"message":      message,
		},
	})
}

// ── Tribulation contribution ──

func (h *Hub) handleContribute(ctx context.Context, c *Client, msg Message) {
	var data struct {
		Type    string `json:"type"`
		Element string `json:"element"`
		Amount  int64  `json:"amount"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "invalid data"})
		return
	}

	// Get active tribulation
	var eventID string
	err := h.db.QueryRow(ctx,
		`SELECT id FROM tribulation_events WHERE status='active' ORDER BY year DESC LIMIT 1`,
	).Scan(&eventID)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "无活跃天劫"})
		return
	}

	switch data.Type {
	case "cultivator":
		var realm string
		var realmLevel int
		h.db.QueryRow(ctx, `SELECT realm, realm_level FROM players WHERE id=$1`, c.playerID).Scan(&realm, &realmLevel)
		combatPower := int64(game.RealmTiers[realm].Order*1000 + realmLevel*100)
		_, _ = h.db.Exec(ctx,
			`INSERT INTO tribulation_contributions (event_id, player_id, type, amount) VALUES ($1,$2,'cultivator',$3)`,
			eventID, c.playerID, combatPower,
		)
		_, _ = h.db.Exec(ctx,
			`UPDATE tribulation_events SET met_cultivators=(SELECT COUNT(DISTINCT player_id) FROM tribulation_contributions WHERE event_id=$1 AND type='cultivator') WHERE id=$1`,
			eventID,
		)
		h.engine.CheckTribulationConditions(ctx, eventID)
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
			Data: map[string]interface{}{"message": fmt.Sprintf("已出战！境界%s，战力%d", game.RealmDisplayName(realm, realmLevel), combatPower)}})

	case "stone":
		if data.Amount <= 0 {
			c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "amount required"})
			return
		}
		var stone int64
		h.db.QueryRow(ctx, `SELECT spirit_stone FROM players WHERE id=$1`, c.playerID).Scan(&stone)
		if stone < data.Amount {
			c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
				Error: fmt.Sprintf("灵石不足，需要%d，当前%d", data.Amount, stone)})
			return
		}
		_, _ = h.db.Exec(ctx, `UPDATE players SET spirit_stone=spirit_stone-$1 WHERE id=$2`, data.Amount, c.playerID)
		_, _ = h.db.Exec(ctx,
			`INSERT INTO tribulation_contributions (event_id, player_id, type, amount) VALUES ($1,$2,'stone',$3)`,
			eventID, c.playerID, data.Amount,
		)
		_, _ = h.db.Exec(ctx,
			`UPDATE tribulation_events SET contributed_spirit_stone=contributed_spirit_stone+$1 WHERE id=$2`,
			data.Amount, eventID,
		)
		h.engine.CheckTribulationConditions(ctx, eventID)
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
			Data: map[string]interface{}{"message": fmt.Sprintf("已贡献%d灵石！", data.Amount)}})

	default:
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown contribution type"})
	}
}

// ── Exploration ──

func (h *Hub) handleExploreStart(ctx context.Context, c *Client, msg Message) {
	var data struct {
		RealmID string `json:"realmId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "invalid data"})
		return
	}

	sr, ok := game.SecretRealms[data.RealmID]
	if !ok {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown secret realm"})
		return
	}

	var activeCount int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM player_explorations WHERE player_id=$1 AND collected=false`, c.playerID).Scan(&activeCount)
	if activeCount > 0 {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "已有进行中的探索，请先结算"})
		return
	}

	var realm string
	var realmLevel int
	var soulSense int64
	h.db.QueryRow(ctx, `SELECT realm, realm_level, soul_sense FROM players WHERE id=$1`, c.playerID).Scan(&realm, &realmLevel, &soulSense)

	if !game.RealmAtLeast(realm, realmLevel, sr.MinRealm, 1) {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("境界不足，需要%s以上", game.RealmTiers[sr.MinRealm].Name)})
		return
	}
	if soulSense < sr.SoulCost {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("神识值不足，需要%d，当前%d", sr.SoulCost, soulSense)})
		return
	}

	_, _ = h.db.Exec(ctx, `UPDATE players SET soul_sense=soul_sense-$1 WHERE id=$2`, sr.SoulCost, c.playerID)
	finishAt := time.Now().Add(time.Duration(sr.DurationSec) * time.Second)
	var exploID string
	h.db.QueryRow(ctx,
		`INSERT INTO player_explorations (player_id, realm_id, finish_at) VALUES ($1,$2,$3) RETURNING id`,
		c.playerID, data.RealmID, finishAt,
	).Scan(&exploID)

	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"explorationId": exploID,
			"realmName":     sr.Name,
			"finishAt":      finishAt,
			"durationSec":   sr.DurationSec,
			"message":       fmt.Sprintf("开始探索【%s】，%d秒后可结算", sr.Name, sr.DurationSec),
		}})
}

func (h *Hub) handleExploreCollect(ctx context.Context, c *Client, msg Message) {
	var exploID, realmID string
	var finishAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT id, realm_id, finish_at FROM player_explorations WHERE player_id=$1 AND collected=false ORDER BY started_at DESC LIMIT 1`,
		c.playerID,
	).Scan(&exploID, &realmID, &finishAt)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "没有待结算的秘境"})
		return
	}
	if time.Now().Before(finishAt) {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("探索未完成，还需%d秒", int(time.Until(finishAt).Seconds()))})
		return
	}

	sr := game.SecretRealms[realmID]
	rewards := game.RollRewards(sr)

	_, _ = h.db.Exec(ctx,
		`UPDATE players SET spirit_stone=spirit_stone+$1, technique_fragment=technique_fragment+$2, updated_at=NOW() WHERE id=$3`,
		rewards["spirit_stone"], rewards["technique_fragment"], c.playerID,
	)
	for _, elem := range game.Elements {
		if qty := rewards["material_"+elem]; qty > 0 {
			_, _ = h.db.Exec(ctx,
				`INSERT INTO spirit_materials (player_id, element, quantity) VALUES ($1,$2,$3)
				 ON CONFLICT (player_id, element) DO UPDATE SET quantity=spirit_materials.quantity+$3`,
				c.playerID, elem, qty,
			)
		}
	}

	_, _ = h.db.Exec(ctx, `UPDATE player_explorations SET collected=true WHERE id=$1`, exploID)

	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"realmName":   sr.Name,
			"spiritStone": rewards["spirit_stone"],
			"message":     fmt.Sprintf("秘境【%s】探索完成！获得灵石%d及天材地宝", sr.Name, rewards["spirit_stone"]),
		}})
}

// ── Alchemy ──

func (h *Hub) handleAlchemyStart(ctx context.Context, c *Client, msg Message) {
	var data struct {
		RecipeID string `json:"recipeId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "invalid data"})
		return
	}

	recipe, ok := game.AlchemyRecipes[data.RecipeID]
	if !ok {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown recipe"})
		return
	}

	var activeCount int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM player_alchemy WHERE player_id=$1 AND collected=false`, c.playerID).Scan(&activeCount)
	if activeCount > 0 {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "已有进行中的炼丹"})
		return
	}

	var realm string
	var realmLevel int
	h.db.QueryRow(ctx, `SELECT realm, realm_level FROM players WHERE id=$1`, c.playerID).Scan(&realm, &realmLevel)
	if !game.RealmAtLeast(realm, realmLevel, recipe.MinRealm, 1) {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("境界不足，需要%s以上", game.RealmTiers[recipe.MinRealm].Name)})
		return
	}

	// Check materials
	matRows, _ := h.db.Query(ctx, `SELECT element, quantity FROM spirit_materials WHERE player_id=$1`, c.playerID)
	materials := map[string]int64{}
	if matRows != nil {
		for matRows.Next() {
			var elem string
			var qty int64
			matRows.Scan(&elem, &qty)
			materials[elem] = qty
		}
		matRows.Close()
	}

	for _, cost := range recipe.MaterialCosts {
		if materials[cost.Element] < cost.Quantity {
			c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
				Error: fmt.Sprintf("%s系天材地宝不足，需要%d，当前%d",
					game.ElementChinese(cost.Element), cost.Quantity, materials[cost.Element])})
			return
		}
	}

	for _, cost := range recipe.MaterialCosts {
		_, _ = h.db.Exec(ctx,
			`UPDATE spirit_materials SET quantity=quantity-$1 WHERE player_id=$2 AND element=$3`,
			cost.Quantity, c.playerID, cost.Element,
		)
	}

	finishAt := time.Now().Add(time.Duration(recipe.DurationSec) * time.Second)
	var alchID string
	h.db.QueryRow(ctx,
		`INSERT INTO player_alchemy (player_id, recipe_id, finish_at) VALUES ($1,$2,$3) RETURNING id`,
		c.playerID, data.RecipeID, finishAt,
	).Scan(&alchID)

	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"alchemyId": alchID, "recipeName": recipe.Name,
			"finishAt": finishAt, "durationSec": recipe.DurationSec,
			"message": fmt.Sprintf("开始炼制【%s】，%d秒后完成", recipe.Name, recipe.DurationSec),
		}})
}

func (h *Hub) handleAlchemyCollect(ctx context.Context, c *Client, msg Message) {
	var alchID, recipeID string
	var finishAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT id, recipe_id, finish_at FROM player_alchemy WHERE player_id=$1 AND collected=false ORDER BY started_at DESC LIMIT 1`,
		c.playerID,
	).Scan(&alchID, &recipeID, &finishAt)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "没有待收取的炼丹"})
		return
	}

	if time.Now().Before(finishAt) {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("炼丹未完成，还需%d秒", int(time.Until(finishAt).Seconds()))})
		return
	}

	recipe := game.AlchemyRecipes[recipeID]
	_, _ = h.db.Exec(ctx, `UPDATE player_alchemy SET collected=true WHERE id=$1`, alchID)

	if recipe.DirectXP > 0 {
		_, _ = h.db.Exec(ctx, `UPDATE players SET cultivation_xp=cultivation_xp+$1 WHERE id=$2`, recipe.DirectXP, c.playerID)
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
			Data: map[string]interface{}{"xpGained": recipe.DirectXP,
				"message": fmt.Sprintf("炼丹完成！【%s】增加%d修为", recipe.Name, recipe.DirectXP)}})
		return
	}

	_, _ = h.db.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity) VALUES ($1,$2,$3)
		 ON CONFLICT (player_id, item_id) DO UPDATE SET quantity=player_items.quantity+$3`,
		c.playerID, recipe.OutputItem, recipe.OutputQty,
	)
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{"item": recipe.OutputItem, "quantity": recipe.OutputQty,
			"message": fmt.Sprintf("炼丹完成！获得【%s】×%d", recipe.Name, recipe.OutputQty)}})
}

// ── Patrol planner ──

func (h *Hub) handlePlanPatrol(ctx context.Context, c *Client, msg Message) {
	var data struct {
		Limit int `json:"limit"`
	}
	json.Unmarshal(msg.Data, &data)
	if data.Limit <= 0 {
		data.Limit = 5
	}

	var p struct {
		Realm         string
		RealmLevel    int
		SpiritStone   int64
		CultivationXP int64
		SoulSense     int64
		SoulSenseMax  int64
		IsCultivating bool
		JoinedEpoch   bool
		Race          string
	}
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level, spirit_stone, cultivation_xp, soul_sense, soul_sense_max, is_cultivating, joined_epoch, race
		 FROM players WHERE id=$1`, c.playerID,
	).Scan(&p.Realm, &p.RealmLevel, &p.SpiritStone, &p.CultivationXP, &p.SoulSense, &p.SoulSenseMax,
		&p.IsCultivating, &p.JoinedEpoch, &p.Race)

	worldStatus, _ := h.engine.GetWorldStatus(ctx)
	xpNeeded := game.GetXPNeeded(p.Realm, p.RealmLevel)

	var actions []map[string]interface{}

	if !p.JoinedEpoch {
		actions = append(actions, map[string]interface{}{
			"action": "cmd.world.join", "urgent": true,
			"reason": "尚未加入美利坚修仙大陆，先踏入这片土地",
		})
	}

	if !p.IsCultivating && p.JoinedEpoch {
		actions = append(actions, map[string]interface{}{
			"action": "cmd.cultivate.start",
			"reason": "未开始闭关，立即开始修炼",
		})
	}

	// Check if tribulation is active
	if ws, ok := worldStatus["activeTribulation"]; ok && ws != nil {
		actions = append(actions, map[string]interface{}{
			"action": "cmd.contribute",
			"data":   map[string]interface{}{"type": "cultivator"},
			"reason": "天劫开启！优先出战贡献！",
			"urgent": true,
		})
	}

	if p.SoulSense >= 20 {
		// Find best secret realm player can access
		for i := len(game.SecretRealmOrder) - 1; i >= 0; i-- {
			sr := game.SecretRealms[game.SecretRealmOrder[i]]
			if game.RealmAtLeast(p.Realm, p.RealmLevel, sr.MinRealm, 1) && p.SoulSense >= sr.SoulCost {
				actions = append(actions, map[string]interface{}{
					"action": "cmd.explore.start",
					"data":   map[string]interface{}{"realmId": sr.ID},
					"reason": fmt.Sprintf("神识充足，探索【%s】获取天材地宝", sr.Name),
				})
				break
			}
		}
	}

	if p.CultivationXP >= xpNeeded {
		actions = append(actions, map[string]interface{}{
			"action": "cmd.breakthrough",
			"reason": fmt.Sprintf("修为已足（%d/%d），立即突破！", p.CultivationXP, xpNeeded),
			"urgent": true,
		})
	}

	if len(actions) > data.Limit {
		actions = actions[:data.Limit]
	}

	if len(actions) == 0 {
		actions = []map[string]interface{}{}
	}

	returnInMinutes := 5 // 1 game year
	c.write(Response{
		Seq:  msg.Seq,
		Type: msg.Type,
		Ok:   true,
		Data: map[string]interface{}{
			"realm":             game.RealmDisplayName(p.Realm, p.RealmLevel),
			"realmLevel":        p.RealmLevel,
			"xpProgress":        fmt.Sprintf("%d/%d", p.CultivationXP, xpNeeded),
			"worldStatus":       worldStatus,
			"actions":           actions,
			"leaveReason":       "已排任务链，服务端按时间流逝自动推进（每5分钟=1游戏年）",
			"returnInMinutes":   returnInMinutes,
			"returnInSeconds":   returnInMinutes * 60,
			"expectedOutcome":   "修为持续增长，天材地宝累积，等待突破时机",
			"wakeTriggers":      []string{"修为足够突破", "天劫开启", "秘境可结算", "炼丹完成"},
		},
	})
}

// ── Cave system ──

func (h *Hub) handleQueryCaves(ctx context.Context, c *Client, msg Message) {
	rows, err := h.db.Query(ctx,
		`SELECT co.cave_id, p.username, p.realm, p.realm_level, co.occupied_at
		 FROM cave_occupations co
		 JOIN players p ON p.id = co.player_id`,
	)
	occupations := map[string]map[string]interface{}{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var caveID, username, realm string
			var realmLevel int
			var occupiedAt time.Time
			rows.Scan(&caveID, &username, &realm, &realmLevel, &occupiedAt)
			occupations[caveID] = map[string]interface{}{
				"username":   username,
				"realmName":  game.RealmDisplayName(realm, realmLevel),
				"occupiedAt": occupiedAt,
			}
		}
		rows.Close()
	}

	var caves []map[string]interface{}
	for _, id := range game.CaveOrder {
		cave := game.LocationCaves[id]
		entry := map[string]interface{}{
			"id":         cave.ID,
			"name":       cave.Name,
			"nameEn":     cave.NameEn,
			"element":    cave.Element,
			"elementCn":  game.ElementChinese(cave.Element),
			"bonusType":  string(cave.BonusType),
			"bonusValue": cave.BonusValue,
			"occupied":   false,
			"occupant":   nil,
		}
		if occ, ok := occupations[id]; ok {
			entry["occupied"] = true
			entry["occupant"] = occ
		}
		caves = append(caves, entry)
	}
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{"caves": caves, "total": len(caves)}})
}

func (h *Hub) handleCaveClaim(ctx context.Context, c *Client, msg Message) {
	var data struct {
		CaveID string `json:"caveId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil || data.CaveID == "" {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "caveId required"})
		return
	}

	cave, ok := game.LocationCaves[data.CaveID]
	if !ok {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown cave"})
		return
	}

	var existingPlayerID string
	err := h.db.QueryRow(ctx, `SELECT player_id FROM cave_occupations WHERE cave_id=$1`, data.CaveID).Scan(&existingPlayerID)
	if err == nil {
		if existingPlayerID == c.playerID {
			c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "你已占领此洞府"})
			return
		}
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "此洞府已被占领，请使用 cmd.cave.challenge"})
		return
	}

	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	_, dbErr := h.db.Exec(ctx,
		`INSERT INTO cave_occupations (cave_id, player_id, last_reward_year) VALUES ($1,$2,$3)
		 ON CONFLICT (cave_id) DO UPDATE SET player_id=$2, occupied_at=NOW(), last_reward_year=$3`,
		data.CaveID, c.playerID, currentYear,
	)
	if dbErr != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "db error"})
		return
	}

	h.engine.PublishCaveEvent(ctx, "cave_claimed", data.CaveID, c.playerID)
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"caveId":    data.CaveID,
			"caveName":  cave.Name,
			"element":   game.ElementChinese(cave.Element),
			"bonusType": string(cave.BonusType),
			"bonusValue": cave.BonusValue,
			"message":   fmt.Sprintf("成功占领【%s】！", cave.Name),
		}})
}

func (h *Hub) handleCaveChallenge(ctx context.Context, c *Client, msg Message) {
	var data struct {
		CaveID string `json:"caveId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil || data.CaveID == "" {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "caveId required"})
		return
	}

	cave, ok := game.LocationCaves[data.CaveID]
	if !ok {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown cave"})
		return
	}

	var defenderID string
	err := h.db.QueryRow(ctx, `SELECT player_id FROM cave_occupations WHERE cave_id=$1`, data.CaveID).Scan(&defenderID)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "洞府无人占领，请用 cmd.cave.claim"})
		return
	}
	if defenderID == c.playerID {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "无法挑战自己"})
		return
	}

	var challRealm, challName string
	var challLevel int
	h.db.QueryRow(ctx, `SELECT realm, realm_level, username FROM players WHERE id=$1`, c.playerID).Scan(&challRealm, &challLevel, &challName)
	var defRealm, defName string
	var defLevel int
	h.db.QueryRow(ctx, `SELECT realm, realm_level, username FROM players WHERE id=$1`, defenderID).Scan(&defRealm, &defLevel, &defName)

	challOrder := game.RealmTiers[challRealm].Order
	defOrder := game.RealmTiers[defRealm].Order

	var challengerWins bool
	if challOrder != defOrder {
		challengerWins = challOrder > defOrder
	} else if challLevel != defLevel {
		challengerWins = challLevel > defLevel
	} else {
		challengerWins = game.RandBool()
	}

	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	if challengerWins {
		_, _ = h.db.Exec(ctx,
			`UPDATE cave_occupations SET player_id=$1, occupied_at=NOW(), last_reward_year=$2 WHERE cave_id=$3`,
			c.playerID, currentYear, data.CaveID,
		)
		h.engine.PublishCaveEvent(ctx, "cave_challenged", data.CaveID, c.playerID)
	}

	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"challengerWins": challengerWins,
			"caveId":         data.CaveID,
			"caveName":       cave.Name,
			"challenger":     challName,
			"defender":       defName,
			"message": func() string {
				if challengerWins {
					return fmt.Sprintf("挑战成功！【%s】已归你所有！", cave.Name)
				}
				return fmt.Sprintf("挑战失败！【%s】守住了！", cave.Name)
			}(),
		}})
}

// ── City realm system ──

func (h *Hub) handleQueryCityRealms(ctx context.Context, c *Client, msg Message) {
	var realms []map[string]interface{}
	for _, id := range game.CityRealmOrder {
		cr := game.CityRealms[id]
		elems := make([]string, len(cr.Elements))
		for i, e := range cr.Elements {
			elems[i] = game.ElementChinese(e)
		}
		realms = append(realms, map[string]interface{}{
			"id":          cr.ID,
			"name":        cr.Name,
			"nameEn":      cr.NameEn,
			"elementsCn":  elems,
			"durationSec": cr.DurationSec,
			"soulCost":    cr.SoulCost,
			"baseXP":      cr.BaseXP,
			"baseStone":   cr.BaseSpiritStone,
		})
	}
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{"cityRealms": realms, "total": len(realms)}})
}

func (h *Hub) handleCityRealmEnter(ctx context.Context, c *Client, msg Message) {
	var data struct {
		CityID string `json:"cityId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil || data.CityID == "" {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "cityId required"})
		return
	}

	cr, ok := game.CityRealms[data.CityID]
	if !ok {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown city realm"})
		return
	}

	var activeCount int
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM city_realm_explorations WHERE player_id=$1 AND collected=false`, c.playerID).Scan(&activeCount)
	if activeCount > 0 {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "已有进行中的城市秘境探索"})
		return
	}

	var soulSense int64
	h.db.QueryRow(ctx, `SELECT soul_sense FROM players WHERE id=$1`, c.playerID).Scan(&soulSense)
	if soulSense < cr.SoulCost {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false,
			Error: fmt.Sprintf("神识值不足，需要%d，当前%d", cr.SoulCost, soulSense)})
		return
	}

	_, _ = h.db.Exec(ctx, `UPDATE players SET soul_sense=soul_sense-$1 WHERE id=$2`, cr.SoulCost, c.playerID)

	_, narrativeSeed := game.RollCityRealmRewards(cr)
	seedJSON, _ := json.Marshal(narrativeSeed)
	finishAt := time.Now().Add(time.Duration(cr.DurationSec) * time.Second)

	var exploID string
	h.db.QueryRow(ctx,
		`INSERT INTO city_realm_explorations (player_id, city_id, finish_at, narrative_seed) VALUES ($1,$2,$3,$4) RETURNING id`,
		c.playerID, data.CityID, finishAt, seedJSON,
	).Scan(&exploID)

	hint := ""
	if h, ok := narrativeSeed["narrative_hint"].(string); ok {
		hint = h
	}

	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"explorationId": exploID,
			"cityId":        data.CityID,
			"cityName":      cr.Name,
			"finishAt":      finishAt,
			"durationSec":   cr.DurationSec,
			"narrativeSeed": narrativeSeed,
			"message":       fmt.Sprintf("踏入【%s】！神识消耗%d，%d秒后可结算。%s", cr.Name, cr.SoulCost, cr.DurationSec, hint),
		}})
}

func (h *Hub) handleCityRealmExit(ctx context.Context, c *Client, msg Message) {
	var data struct {
		CityID string `json:"cityId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil || data.CityID == "" {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "cityId required"})
		return
	}

	cr, ok := game.CityRealms[data.CityID]
	if !ok {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "unknown city realm"})
		return
	}

	var exploID string
	var finishAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT id, finish_at FROM city_realm_explorations WHERE player_id=$1 AND city_id=$2 AND collected=false ORDER BY started_at DESC LIMIT 1`,
		c.playerID, data.CityID,
	).Scan(&exploID, &finishAt)
	if err != nil {
		c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: false, Error: "没有进行中的探索"})
		return
	}

	rewards, narrativeSeed := game.RollCityRealmRewards(cr)
	isEarly := time.Now().Before(finishAt)
	if isEarly {
		elapsed := time.Since(finishAt.Add(-time.Duration(cr.DurationSec) * time.Second))
		ratio := float64(elapsed.Seconds()) / float64(cr.DurationSec)
		if ratio < 0 { ratio = 0 }
		if ratio > 1 { ratio = 1 }
		for k, v := range rewards {
			rewards[k] = int64(float64(v) * ratio)
		}
	}

	if xp := rewards["cultivation_xp"]; xp > 0 {
		_, _ = h.db.Exec(ctx, `UPDATE players SET cultivation_xp=cultivation_xp+$1 WHERE id=$2`, xp, c.playerID)
	}
	if stone := rewards["spirit_stone"]; stone > 0 {
		_, _ = h.db.Exec(ctx, `UPDATE players SET spirit_stone=spirit_stone+$1 WHERE id=$2`, stone, c.playerID)
	}
	for _, elem := range game.Elements {
		if qty := rewards["material_"+elem]; qty > 0 {
			_, _ = h.db.Exec(ctx,
				`INSERT INTO spirit_materials (player_id, element, quantity) VALUES ($1,$2,$3) ON CONFLICT (player_id,element) DO UPDATE SET quantity=spirit_materials.quantity+$3`,
				c.playerID, elem, qty,
			)
		}
	}
	_, _ = h.db.Exec(ctx, `UPDATE city_realm_explorations SET collected=true WHERE id=$1`, exploID)
	h.engine.PublishRealmCompleteEvent(ctx, data.CityID, c.playerID)

	hint := ""
	if h, ok := narrativeSeed["narrative_hint"].(string); ok {
		hint = h
	}
	c.write(Response{Seq: msg.Seq, Type: msg.Type, Ok: true,
		Data: map[string]interface{}{
			"cityId":        data.CityID,
			"cityName":      cr.Name,
			"earlyExit":     isEarly,
			"cultivationXp": rewards["cultivation_xp"],
			"spiritStone":   rewards["spirit_stone"],
			"narrativeSeed": narrativeSeed,
			"message":       fmt.Sprintf("【%s】秘境结算！修为+%d，灵石+%d。%s", cr.Name, rewards["cultivation_xp"], rewards["spirit_stone"], hint),
		}})
}
