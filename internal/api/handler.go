package api

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/xiantu/server/internal/auth"
	"github.com/xiantu/server/internal/game"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	db        *pgxpool.Pool
	rdb       *redis.Client
	engine    *game.Engine
	jwtSecret string
}

func NewHandler(db *pgxpool.Pool, rdb *redis.Client, engine *game.Engine, jwtSecret string) *Handler {
	return &Handler{db: db, rdb: rdb, engine: engine, jwtSecret: jwtSecret}
}

// AuthMiddleware extracts and validates JWT
func (h *Handler) AuthMiddleware(c *fiber.Ctx) error {
	tokenStr := c.Get("Authorization")
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	if tokenStr == "" {
		return c.Status(401).JSON(fiber.Map{"error": "Missing or invalid token"})
	}

	claims, err := auth.ParseToken(tokenStr, h.jwtSecret)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid token"})
	}

	c.Locals("playerID", claims.PlayerID)
	c.Locals("agentID", claims.AgentID)
	return c.Next()
}

// OptionalAuth tries to parse JWT but doesn't fail if missing
func (h *Handler) OptionalAuth(c *fiber.Ctx) error {
	tokenStr := c.Get("Authorization")
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	if tokenStr != "" {
		if claims, err := auth.ParseToken(tokenStr, h.jwtSecret); err == nil {
			c.Locals("playerID", claims.PlayerID)
			c.Locals("agentID", claims.AgentID)
		}
	}
	return c.Next()
}

// ========== Auth ==========

// POST /api/register
func (h *Handler) Register(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Race     string `json:"race"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.Username == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"error": "username and password required"})
	}
	if len(req.Username) < 3 || len(req.Username) > 20 {
		return c.Status(400).JSON(fiber.Map{"error": "username must be 3-20 chars"})
	}

	if req.Race == "" {
		req.Race = "chinese"
	}
	if _, ok := game.Races[req.Race]; !ok {
		return c.Status(400).JSON(fiber.Map{
			"error":      fmt.Sprintf("invalid race, valid options: %s", strings.Join(game.RaceOrder, ", ")),
			"validRaces": game.RaceOrder,
		})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "internal error"})
	}

	rootName, multiplier := game.RollSpiritRoot()
	agentID := "agt-" + uuid.New().String()[:12]

	ctx := context.Background()
	var playerID string
	err = h.db.QueryRow(ctx,
		`INSERT INTO players (username, password_hash, agent_id, spirit_root, spirit_root_multiplier, race)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		req.Username, string(hash), agentID, rootName, multiplier, req.Race,
	).Scan(&playerID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return c.Status(409).JSON(fiber.Map{"error": "username already exists"})
		}
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("db error: %v", err)})
	}

	token, expiresAt, err := auth.GenerateToken(playerID, agentID, h.jwtSecret)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "token error"})
	}

	raceInfo := game.Races[req.Race]
	rootDisplayName := game.SpiritRootNames[rootName]

	// Get world status for new player greeting
	worldStatus, _ := h.engine.GetWorldStatus(ctx)

	return c.Status(201).JSON(fiber.Map{
		"playerId":             playerID,
		"agentId":              agentID,
		"token":                token,
		"expiresAt":            expiresAt,
		"spiritRoot":           rootName,
		"spiritRootName":       rootDisplayName,
		"spiritRootMultiplier": multiplier,
		"race":                 req.Race,
		"raceName":             raceInfo.Name,
		"raceSpecialName":      raceInfo.SpecialName,
		"raceSpecialDesc":      raceInfo.SpecialDesc,
		"worldStatus":          worldStatus,
		"message": fmt.Sprintf("恭喜！你的灵根为【%s】（修炼速度×%.1f），族裔为【%s】，欢迎踏入《黑人修仙传》！",
			rootDisplayName, multiplier, raceInfo.Name),
	})
}

// POST /api/login
func (h *Handler) Login(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	ctx := context.Background()
	var playerID, passwordHash, agentID string
	err := h.db.QueryRow(ctx,
		`SELECT id, password_hash, agent_id FROM players WHERE username=$1`,
		req.Username,
	).Scan(&playerID, &passwordHash, &agentID)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
	}

	token, expiresAt, err := auth.GenerateToken(playerID, agentID, h.jwtSecret)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "token error"})
	}

	return c.JSON(fiber.Map{
		"token":     token,
		"expiresAt": expiresAt,
		"playerId":  playerID,
		"agentId":   agentID,
	})
}

// GET /api/profile
func (h *Handler) Profile(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	var p struct {
		ID                string
		Username          string
		AgentID           string
		SpiritRoot        string
		Multiplier        float64
		Race              string
		Realm             string
		RealmLevel        int
		SpiritStone       int64
		CultivationXP     int64
		TechniqueFragment int64
		SoulSense         int64
		SoulSenseMax      int64
		CaveLevel         int
		EquippedTechnique string
		IsCultivating     bool
		JoinedEpoch       bool
		CreatedAt         time.Time
	}

	err := h.db.QueryRow(ctx,
		`SELECT id, username, agent_id, spirit_root, spirit_root_multiplier, race,
		 realm, realm_level, spirit_stone, cultivation_xp, technique_fragment,
		 soul_sense, soul_sense_max, cave_level, equipped_technique,
		 is_cultivating, joined_epoch, created_at
		 FROM players WHERE id=$1`,
		playerID,
	).Scan(&p.ID, &p.Username, &p.AgentID, &p.SpiritRoot, &p.Multiplier, &p.Race,
		&p.Realm, &p.RealmLevel, &p.SpiritStone, &p.CultivationXP, &p.TechniqueFragment,
		&p.SoulSense, &p.SoulSenseMax, &p.CaveLevel, &p.EquippedTechnique,
		&p.IsCultivating, &p.JoinedEpoch, &p.CreatedAt)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "player not found"})
	}

	// Get spirit materials
	materials := h.getSpiritMaterials(ctx, playerID)

	realmInfo := game.RealmTiers[p.Realm]
	raceInfo := game.Races[p.Race]
	xpNeeded := game.GetXPNeeded(p.Realm, p.RealmLevel)

	worldStatus, _ := h.engine.GetWorldStatus(ctx)

	return c.JSON(fiber.Map{
		"id":                   p.ID,
		"username":             p.Username,
		"agentId":              p.AgentID,
		"spiritRoot":           p.SpiritRoot,
		"spiritRootName":       game.SpiritRootNames[p.SpiritRoot],
		"spiritRootMultiplier": p.Multiplier,
		"race":                 p.Race,
		"raceName":             raceInfo.Name,
		"raceSpecial":          raceInfo.SpecialName,
		"realm":                p.Realm,
		"realmName":            game.RealmDisplayName(p.Realm, p.RealmLevel),
		"realmTier":            realmInfo.Name,
		"realmLevel":           p.RealmLevel,
		"resources": fiber.Map{
			"spiritStone":       p.SpiritStone,
			"cultivationXp":     p.CultivationXP,
			"spiritMaterials":   materials,
			"techniqueFragment": p.TechniqueFragment,
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
		"createdAt":         p.CreatedAt,
	})
}

func (h *Handler) getSpiritMaterials(ctx context.Context, playerID string) map[string]int64 {
	result := map[string]int64{"metal": 0, "wood": 0, "water": 0, "fire": 0, "earth": 0}
	rows, err := h.db.Query(ctx,
		`SELECT element, quantity FROM spirit_materials WHERE player_id=$1`,
		playerID,
	)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var elem string
		var qty int64
		rows.Scan(&elem, &qty)
		result[elem] = qty
	}
	return result
}

func (h *Handler) addSpiritMaterial(ctx context.Context, playerID, element string, qty int64) error {
	_, err := h.db.Exec(ctx,
		`INSERT INTO spirit_materials (player_id, element, quantity) VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, element) DO UPDATE SET quantity=spirit_materials.quantity+$3`,
		playerID, element, qty,
	)
	return err
}

// ========== World API ==========

// GET /api/world/status
func (h *Handler) WorldStatus(c *fiber.Ctx) error {
	ctx := context.Background()
	status, err := h.engine.GetWorldStatus(ctx)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to get world status"})
	}

	// Add hall of fame summary
	var lastHof struct {
		Year    int
		Element string
	}
	h.db.QueryRow(ctx,
		`SELECT year, element FROM hall_of_fame ORDER BY recorded_at DESC LIMIT 1`,
	).Scan(&lastHof.Year, &lastHof.Element)
	if lastHof.Year > 0 {
		status["lastTribulationYear"] = lastHof.Year
		status["lastTribulationElement"] = lastHof.Element
		status["lastTribulationElementCn"] = game.ElementChinese(lastHof.Element)
	}

	return c.JSON(status)
}

// GET /api/world/tribulation
func (h *Handler) TribulationStatus(c *fiber.Ctx) error {
	ctx := context.Background()

	var tribID, element, status string
	var year int
	var windowStart, windowEnd time.Time
	var reqCultRealm string
	var reqCultLevel, reqCultCount int
	var reqStone int64
	var reqMatRatio int
	var metCultivators int
	var contribStone int64

	err := h.db.QueryRow(ctx,
		`SELECT id, year, element, status, window_start_at, window_end_at,
		        req_cultivator_realm, req_cultivator_level, req_cultivator_count,
		        req_spirit_stone, req_material_ratio,
		        met_cultivators, contributed_spirit_stone
		 FROM tribulation_events WHERE status='active'
		 ORDER BY year DESC LIMIT 1`,
	).Scan(&tribID, &year, &element, &status,
		&windowStart, &windowEnd,
		&reqCultRealm, &reqCultLevel, &reqCultCount,
		&reqStone, &reqMatRatio,
		&metCultivators, &contribStone)
	if err != nil {
		return c.JSON(fiber.Map{
			"active": false,
			"message": "无活跃天劫",
		})
	}

	// Get material contributions
	matContribs := map[string]int64{}
	rows, _ := h.db.Query(ctx,
		`SELECT element, SUM(amount) FROM tribulation_contributions
		 WHERE event_id=$1 AND type='material' GROUP BY element`,
		tribID,
	)
	if rows != nil {
		defer rows.Close()
		var totalMat int64
		for rows.Next() {
			var elem string
			var amt int64
			rows.Scan(&elem, &amt)
			matContribs[elem] = amt
			totalMat += amt
		}
		rows.Close()

		var elemMat int64
		for k, v := range matContribs {
			if k == element {
				elemMat = v
			}
		}
		currentRatio := 0
		if totalMat > 0 {
			currentRatio = int(elemMat * 100 / totalMat)
		}

		remaining := time.Until(windowEnd).Seconds()
		return c.JSON(fiber.Map{
			"active":    true,
			"eventId":   tribID,
			"year":      year,
			"element":   element,
			"elementCn": game.ElementChinese(element),
			"status":    status,
			"windowStartAt": windowStart,
			"windowEndAt":   windowEnd,
			"remainingSec":  int(remaining),
			"conditions": fiber.Map{
				"cultivators": fiber.Map{
					"required":   reqCultCount,
					"current":    metCultivators,
					"met":        metCultivators >= reqCultCount,
					"minRealm":   reqCultRealm,
					"minRealmCn": game.RealmDisplayName(reqCultRealm, reqCultLevel),
				},
				"spiritStone": fiber.Map{
					"required":    reqStone,
					"contributed": contribStone,
					"met":         contribStone >= reqStone,
				},
				"materials": fiber.Map{
					"requiredElement":   element,
					"requiredElementCn": game.ElementChinese(element),
					"requiredRatioPct":  reqMatRatio,
					"currentRatioPct":   currentRatio,
					"met":               currentRatio >= reqMatRatio,
					"breakdown":         matContribs,
				},
			},
		})
	}

	return c.JSON(fiber.Map{"active": false})
}

// POST /api/world/contribute
func (h *Handler) Contribute(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	var req struct {
		Type    string `json:"type"`    // "cultivator", "stone", "material"
		Element string `json:"element"` // for material type
		Amount  int64  `json:"amount"`  // for stone/material
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	ctx := context.Background()

	// Get active tribulation
	var eventID, tribElement, status string
	var reqCultRealm string
	var reqCultLevel int
	err := h.db.QueryRow(ctx,
		`SELECT id, element, status, req_cultivator_realm, req_cultivator_level
		 FROM tribulation_events WHERE status='active' ORDER BY year DESC LIMIT 1`,
	).Scan(&eventID, &tribElement, &status, &reqCultRealm, &reqCultLevel)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "无活跃天劫"})
	}

	var realm string
	var realmLevel int
	var spiritStone int64
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level, spirit_stone FROM players WHERE id=$1`,
		playerID,
	).Scan(&realm, &realmLevel, &spiritStone)

	switch req.Type {
	case "cultivator":
		// Check realm requirement
		if !game.RealmAtLeast(realm, realmLevel, reqCultRealm, reqCultLevel) {
			return c.Status(400).JSON(fiber.Map{
				"error": fmt.Sprintf("境界不足，天劫要求%s以上", game.RealmDisplayName(reqCultRealm, reqCultLevel)),
			})
		}
		// Add contribution
		combatPower := int64(game.RealmTiers[realm].Order*1000 + realmLevel*100)
		_, err = h.db.Exec(ctx,
			`INSERT INTO tribulation_contributions (event_id, player_id, type, amount)
			 VALUES ($1, $2, 'cultivator', $3)
			 ON CONFLICT DO NOTHING`,
			eventID, playerID, combatPower,
		)
		// Update met_cultivators count
		_, _ = h.db.Exec(ctx,
			`UPDATE tribulation_events SET met_cultivators=(
			   SELECT COUNT(DISTINCT player_id) FROM tribulation_contributions
			   WHERE event_id=$1 AND type='cultivator'
			 ) WHERE id=$1`, eventID,
		)
		// Check all conditions
		h.engine.CheckTribulationConditions(ctx, eventID)

		return c.JSON(fiber.Map{
			"type":        "cultivator",
			"combatPower": combatPower,
			"realmName":   game.RealmDisplayName(realm, realmLevel),
			"message":     fmt.Sprintf("已出战！境界【%s】，战力%d", game.RealmDisplayName(realm, realmLevel), combatPower),
		})

	case "stone":
		if req.Amount <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "amount must be positive"})
		}
		if spiritStone < req.Amount {
			return c.Status(400).JSON(fiber.Map{
				"error": fmt.Sprintf("灵石不足，需要%d，当前%d", req.Amount, spiritStone),
			})
		}
		// Deduct and contribute
		_, _ = h.db.Exec(ctx,
			`UPDATE players SET spirit_stone=spirit_stone-$1, updated_at=NOW() WHERE id=$2`,
			req.Amount, playerID,
		)
		_, _ = h.db.Exec(ctx,
			`INSERT INTO tribulation_contributions (event_id, player_id, type, amount)
			 VALUES ($1, $2, 'stone', $3)`,
			eventID, playerID, req.Amount,
		)
		_, _ = h.db.Exec(ctx,
			`UPDATE tribulation_events SET contributed_spirit_stone=contributed_spirit_stone+$1 WHERE id=$2`,
			req.Amount, eventID,
		)
		h.engine.CheckTribulationConditions(ctx, eventID)

		return c.JSON(fiber.Map{
			"type":    "stone",
			"amount":  req.Amount,
			"message": fmt.Sprintf("已贡献%d灵石给天劫！", req.Amount),
		})

	case "material":
		if req.Element == "" {
			return c.Status(400).JSON(fiber.Map{"error": "element required for material contribution"})
		}
		if req.Amount <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "amount must be positive"})
		}
		// Check player has enough materials
		var matQty int64
		h.db.QueryRow(ctx,
			`SELECT COALESCE(quantity, 0) FROM spirit_materials WHERE player_id=$1 AND element=$2`,
			playerID, req.Element,
		).Scan(&matQty)
		if matQty < req.Amount {
			return c.Status(400).JSON(fiber.Map{
				"error": fmt.Sprintf("%s系天材地宝不足，需要%d，当前%d",
					game.ElementChinese(req.Element), req.Amount, matQty),
			})
		}
		// Deduct and contribute
		_, _ = h.db.Exec(ctx,
			`UPDATE spirit_materials SET quantity=quantity-$1 WHERE player_id=$2 AND element=$3`,
			req.Amount, playerID, req.Element,
		)
		_, _ = h.db.Exec(ctx,
			`INSERT INTO tribulation_contributions (event_id, player_id, type, amount, element)
			 VALUES ($1, $2, 'material', $3, $4)`,
			eventID, playerID, req.Amount, req.Element,
		)
		h.engine.CheckTribulationConditions(ctx, eventID)

		return c.JSON(fiber.Map{
			"type":      "material",
			"element":   req.Element,
			"elementCn": game.ElementChinese(req.Element),
			"amount":    req.Amount,
			"message":   fmt.Sprintf("已贡献%d个%s系天材地宝给天劫！", req.Amount, game.ElementChinese(req.Element)),
		})

	default:
		return c.Status(400).JSON(fiber.Map{"error": "type must be 'cultivator', 'stone', or 'material'"})
	}
}

// GET /api/world/hall-of-fame
func (h *Handler) HallOfFame(c *fiber.Ctx) error {
	ctx := context.Background()

	rows, err := h.db.Query(ctx,
		`SELECT hf.year, hf.element, hf.recorded_at,
		        te.req_cultivator_count, te.met_cultivators, te.contributed_spirit_stone
		 FROM hall_of_fame hf
		 JOIN tribulation_events te ON te.id=hf.tribulation_event_id
		 ORDER BY hf.year DESC LIMIT 20`,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}
	defer rows.Close()

	var entries []fiber.Map
	for rows.Next() {
		var year int
		var element string
		var recordedAt time.Time
		var reqCount, metCount int
		var contribStone int64
		rows.Scan(&year, &element, &recordedAt, &reqCount, &metCount, &contribStone)
		entries = append(entries, fiber.Map{
			"year":             year,
			"element":          element,
			"elementCn":        game.ElementChinese(element),
			"recordedAt":       recordedAt,
			"cultivatorCount":  metCount,
			"spiritStoneTotal": contribStone,
		})
	}

	if entries == nil {
		entries = []fiber.Map{}
	}

	return c.JSON(fiber.Map{"hallOfFame": entries})
}

// ========== Game Data API ==========

// GET /api/realms
func (h *Handler) Realms(c *fiber.Ctx) error {
	var realms []fiber.Map
	for _, id := range game.RealmOrder {
		tier := game.RealmTiers[id]
		realms = append(realms, fiber.Map{
			"id":                   tier.ID,
			"name":                 tier.Name,
			"maxLevel":             tier.MaxLevel,
			"levelNames":           tier.LevelNames,
			"xpPerLevel":           tier.XPPerLevel,
			"breakthroughItem":     tier.BreakthroughItem,
			"breakthroughItemName": tier.BreakthroughItemName,
		})
	}
	return c.JSON(fiber.Map{"realms": realms})
}

// GET /api/races
func (h *Handler) RaceList(c *fiber.Ctx) error {
	var races []fiber.Map
	for _, id := range game.RaceOrder {
		r := game.Races[id]
		races = append(races, fiber.Map{
			"id":                   r.ID,
			"name":                 r.Name,
			"desc":                 r.Desc,
			"elementBonus":         r.ElementBonus,
			"specialName":          r.SpecialName,
			"specialDesc":          r.SpecialDesc,
			"cultivationSpeedPct":  r.CultivationSpeedPct,
			"idleCultivationPct":   r.IdleCultivationPct,
			"breakthroughBonusPct": r.BreakthroughBonusPct,
			"breakFailProtect":     r.BreakFailProtect,
			"refiningQualityPct":   r.RefiningQualityPct,
			"fireTechSpeedPct":     r.FireTechSpeedPct,
		})
	}
	return c.JSON(fiber.Map{"races": races})
}

// ========== Cultivation API ==========

// POST /api/cultivate/offline
func (h *Handler) OfflineCultivation(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	var req struct {
		Duration int64 `json:"duration"` // seconds
	}
	if err := c.BodyParser(&req); err != nil || req.Duration <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "duration (seconds) required and must be positive"})
	}
	if req.Duration > 86400 {
		req.Duration = 86400 // cap at 24 hours
	}

	ctx := context.Background()
	var rootMultiplier float64
	var race, equippedTech string
	var caveLevel int
	var lastClaim time.Time
	err := h.db.QueryRow(ctx,
		`SELECT spirit_root_multiplier, race, cave_level, equipped_technique, last_offline_claim
		 FROM players WHERE id=$1`,
		playerID,
	).Scan(&rootMultiplier, &race, &caveLevel, &equippedTech, &lastClaim)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "player not found"})
	}

	// Must wait at least 5 minutes between claims
	if time.Since(lastClaim) < 5*time.Minute {
		return c.Status(429).JSON(fiber.Map{"error": "离线收益每5分钟只能领取一次（1游戏年）"})
	}

	// Calculate years offline
	yearsOffline := req.Duration / int64(game.GameYearDuration.Seconds())
	if yearsOffline < 1 {
		yearsOffline = 1
	}
	xpPerYear := game.CalcXPPerYear(rootMultiplier, race, caveLevel, equippedTech)
	totalXP := xpPerYear * yearsOffline

	_, err = h.db.Exec(ctx,
		`UPDATE players SET cultivation_xp=cultivation_xp+$1, last_offline_claim=NOW(), updated_at=NOW() WHERE id=$2`,
		totalXP, playerID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	return c.JSON(fiber.Map{
		"offlineDuration": req.Duration,
		"yearsCalculated": yearsOffline,
		"xpPerYear":       xpPerYear,
		"totalXpEarned":   totalXP,
		"message": fmt.Sprintf("离线修炼%d秒（约%d游戏年），获得%d修为",
			req.Duration, yearsOffline, totalXP),
	})
}

// POST /api/breakthrough
func (h *Handler) Breakthrough(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	var xp int64
	var realm, race string
	var realmLevel int
	err := h.db.QueryRow(ctx,
		`SELECT cultivation_xp, realm, realm_level, race FROM players WHERE id=$1`,
		playerID,
	).Scan(&xp, &realm, &realmLevel, &race)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "player not found"})
	}

	needed := game.GetXPNeeded(realm, realmLevel)
	if xp < needed {
		return c.Status(400).JSON(fiber.Map{
			"error": fmt.Sprintf("修为不足，需要%d，当前%d，还差%d", needed, xp, needed-xp),
		})
	}

	newRealm, newLevel, isMajor := game.NextRealmLevel(realm, realmLevel)
	if newRealm == "" {
		return c.Status(400).JSON(fiber.Map{"error": "已达最高境界"})
	}

	raceInfo := game.Races[race]

	if isMajor {
		nextTier := game.RealmTiers[newRealm]
		if nextTier.BreakthroughItem != "" {
			var qty int
			err = h.db.QueryRow(ctx,
				`SELECT COALESCE(quantity, 0) FROM player_items WHERE player_id=$1 AND item_id=$2`,
				playerID, nextTier.BreakthroughItem,
			).Scan(&qty)
			if err != nil || qty < 1 {
				return c.Status(400).JSON(fiber.Map{
					"error": fmt.Sprintf("缺少突破道具【%s】，请通过炼丹获取", nextTier.BreakthroughItemName),
				})
			}
			_, _ = h.db.Exec(ctx,
				`UPDATE player_items SET quantity=quantity-1 WHERE player_id=$1 AND item_id=$2`,
				playerID, nextTier.BreakthroughItem,
			)
		}

		successRate := game.BreakthroughBaseRate + raceInfo.BreakthroughBonusPct
		roll := rand.Intn(100)
		if roll >= successRate {
			var xpLost int64
			message := fmt.Sprintf("突破失败！成功率%d%%，骰子%d", successRate, roll)

			loseXP := rand.Intn(100) < game.BreakthroughFailLossChance && !raceInfo.BreakFailProtect
			if !loseXP && raceInfo.BreakFailProtect && rand.Intn(100) < 30 {
				loseXP = false // 30% chance to trigger protection
			}

			if loseXP {
				xpLost = xp * int64(game.BreakthroughFailXPLossPct) / 100
				_, _ = h.db.Exec(ctx,
					`UPDATE players SET cultivation_xp=cultivation_xp-$1, updated_at=NOW() WHERE id=$2`,
					xpLost, playerID,
				)
				message += fmt.Sprintf("，损失%d修为（%d%%）", xpLost, game.BreakthroughFailXPLossPct)
			} else if raceInfo.BreakFailProtect {
				message += "，族裔天赋【不屈根基】保护，未损失修为"
			} else {
				message += "，幸运地未损失修为"
			}

			return c.JSON(fiber.Map{
				"success":      false,
				"realm":        realm,
				"realmLevel":   realmLevel,
				"xpLost":       xpLost,
				"itemConsumed": nextTier.BreakthroughItemName,
				"message":      message,
			})
		}
	}

	newXP := xp - needed
	_, err = h.db.Exec(ctx,
		`UPDATE players SET realm=$1, realm_level=$2, cultivation_xp=$3, updated_at=NOW() WHERE id=$4`,
		newRealm, newLevel, newXP, playerID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	displayName := game.RealmDisplayName(newRealm, newLevel)
	message := fmt.Sprintf("突破成功！进阶至【%s】！剩余修为：%d", displayName, newXP)
	if isMajor {
		message = fmt.Sprintf("大突破成功！踏入【%s】！天地灵气涌动！剩余修为：%d", displayName, newXP)
	}

	return c.JSON(fiber.Map{
		"success":      true,
		"prevRealm":    realm,
		"prevLevel":    realmLevel,
		"newRealm":     newRealm,
		"newRealmName": displayName,
		"newLevel":     newLevel,
		"xpConsumed":   needed,
		"xpRemaining":  newXP,
		"message":      message,
	})
}

// ========== Techniques ==========

// GET /api/techniques
func (h *Handler) TechniqueList(c *fiber.Ctx) error {
	var techniques []fiber.Map
	for _, id := range game.TechniqueOrder {
		t := game.Techniques[id]
		techniques = append(techniques, fiber.Map{
			"id":         t.ID,
			"name":       t.Name,
			"element":    t.Element,
			"minRealm":   t.MinRealm,
			"minLevel":   t.MinLevel,
			"fragCost":   t.FragCost,
			"xpBonusPct": t.XPBonusPct,
			"desc":       t.Desc,
		})
	}
	return c.JSON(fiber.Map{"techniques": techniques})
}

// POST /api/technique/equip
func (h *Handler) EquipTechnique(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	var req struct {
		TechniqueID string `json:"techniqueId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	tech, ok := game.Techniques[req.TechniqueID]
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "unknown technique"})
	}

	ctx := context.Background()
	var realm string
	var realmLevel int
	var fragments int64
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level, technique_fragment FROM players WHERE id=$1`,
		playerID,
	).Scan(&realm, &realmLevel, &fragments)

	if !game.RealmAtLeast(realm, realmLevel, tech.MinRealm, tech.MinLevel) {
		return c.Status(400).JSON(fiber.Map{
			"error": fmt.Sprintf("境界不足，需要%s", game.RealmDisplayName(tech.MinRealm, tech.MinLevel)),
		})
	}

	var learned bool
	h.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM player_techniques WHERE player_id=$1 AND technique_id=$2)`,
		playerID, req.TechniqueID,
	).Scan(&learned)

	if !learned {
		if tech.FragCost > 0 && fragments < int64(tech.FragCost) {
			return c.Status(400).JSON(fiber.Map{
				"error": fmt.Sprintf("功法残页不足，需要%d，当前%d", tech.FragCost, fragments),
			})
		}
		if tech.FragCost > 0 {
			_, _ = h.db.Exec(ctx,
				`UPDATE players SET technique_fragment=technique_fragment-$1, updated_at=NOW() WHERE id=$2`,
				tech.FragCost, playerID,
			)
		}
		_, _ = h.db.Exec(ctx,
			`INSERT INTO player_techniques (player_id, technique_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			playerID, req.TechniqueID,
		)
	}

	_, err := h.db.Exec(ctx,
		`UPDATE players SET equipped_technique=$1, updated_at=NOW() WHERE id=$2`,
		req.TechniqueID, playerID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	return c.JSON(fiber.Map{
		"equipped":     req.TechniqueID,
		"name":         tech.Name,
		"xpBonusPct":   tech.XPBonusPct,
		"newlyLearned": !learned,
		"message":      fmt.Sprintf("已装备功法【%s】，修炼速度+%.0f%%", tech.Name, tech.XPBonusPct),
	})
}

// ========== Secret Realms ==========

// GET /api/secret-realms
func (h *Handler) SecretRealmList(c *fiber.Ctx) error {
	var realms []fiber.Map
	for _, id := range game.SecretRealmOrder {
		sr := game.SecretRealms[id]
		realms = append(realms, fiber.Map{
			"id":          sr.ID,
			"name":        sr.Name,
			"minRealm":    sr.MinRealm,
			"soulCost":    sr.SoulCost,
			"durationSec": sr.DurationSec,
			"rewards": fiber.Map{
				"spiritStone":      fiber.Map{"min": sr.Rewards.SpiritStone[0], "max": sr.Rewards.SpiritStone[1]},
				"materialPerElem":  fiber.Map{"min": sr.Rewards.MaterialPerElem[0], "max": sr.Rewards.MaterialPerElem[1]},
				"techFragment":     fiber.Map{"min": sr.Rewards.TechFragment[0], "max": sr.Rewards.TechFragment[1]},
			},
			"desc": sr.Desc,
		})
	}
	return c.JSON(fiber.Map{"secretRealms": realms})
}

// POST /api/secret-realm/explore
func (h *Handler) ExploreSecretRealm(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	var req struct {
		RealmID string `json:"realmId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	sr, ok := game.SecretRealms[req.RealmID]
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "unknown secret realm"})
	}

	ctx := context.Background()

	var activeCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM player_explorations WHERE player_id=$1 AND collected=false`,
		playerID,
	).Scan(&activeCount)
	if activeCount > 0 {
		return c.Status(400).JSON(fiber.Map{"error": "已有正在进行的秘境探索，请先结算"})
	}

	var realm string
	var realmLevel int
	var soulSense int64
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level, soul_sense FROM players WHERE id=$1`,
		playerID,
	).Scan(&realm, &realmLevel, &soulSense)

	if !game.RealmAtLeast(realm, realmLevel, sr.MinRealm, 1) {
		minTier := game.RealmTiers[sr.MinRealm]
		return c.Status(400).JSON(fiber.Map{
			"error": fmt.Sprintf("境界不足，需要%s以上", minTier.Name),
		})
	}

	if soulSense < sr.SoulCost {
		return c.Status(400).JSON(fiber.Map{
			"error": fmt.Sprintf("神识值不足，需要%d，当前%d", sr.SoulCost, soulSense),
		})
	}

	_, _ = h.db.Exec(ctx,
		`UPDATE players SET soul_sense=soul_sense-$1, updated_at=NOW() WHERE id=$2`,
		sr.SoulCost, playerID,
	)

	finishAt := time.Now().Add(time.Duration(sr.DurationSec) * time.Second)
	var explorationID string
	h.db.QueryRow(ctx,
		`INSERT INTO player_explorations (player_id, realm_id, finish_at) VALUES ($1, $2, $3) RETURNING id`,
		playerID, req.RealmID, finishAt,
	).Scan(&explorationID)

	return c.JSON(fiber.Map{
		"explorationId": explorationID,
		"realmId":       req.RealmID,
		"realmName":     sr.Name,
		"durationSec":   sr.DurationSec,
		"finishAt":      finishAt,
		"message":       fmt.Sprintf("开始探索【%s】，%d秒后可结算收益", sr.Name, sr.DurationSec),
	})
}

// GET /api/secret-realm/collect
func (h *Handler) CollectExploration(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	var explorationID, realmID string
	var finishAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT id, realm_id, finish_at FROM player_explorations
		 WHERE player_id=$1 AND collected=false ORDER BY started_at DESC LIMIT 1`,
		playerID,
	).Scan(&explorationID, &realmID, &finishAt)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "没有待结算的秘境探索"})
	}

	if time.Now().Before(finishAt) {
		remaining := time.Until(finishAt).Seconds()
		return c.Status(400).JSON(fiber.Map{
			"error":        "探索尚未完成",
			"remainingSec": int(remaining),
		})
	}

	sr := game.SecretRealms[realmID]
	rewards := game.RollRewards(sr)

	// Apply stone and fragment rewards
	_, _ = h.db.Exec(ctx,
		`UPDATE players SET
		 spirit_stone=spirit_stone+$1,
		 technique_fragment=technique_fragment+$2,
		 updated_at=NOW() WHERE id=$3`,
		rewards["spirit_stone"], rewards["technique_fragment"], playerID,
	)

	// Apply element materials
	materialSummary := map[string]int64{}
	for _, elem := range game.Elements {
		key := "material_" + elem
		if qty, ok := rewards[key]; ok && qty > 0 {
			_ = h.addSpiritMaterial(ctx, playerID, elem, qty)
			materialSummary[elem] = qty
		}
	}

	_, _ = h.db.Exec(ctx,
		`UPDATE player_explorations SET collected=true WHERE id=$1`,
		explorationID,
	)

	return c.JSON(fiber.Map{
		"realmName": sr.Name,
		"rewards": fiber.Map{
			"spiritStone":       rewards["spirit_stone"],
			"techniqueFragment": rewards["technique_fragment"],
			"spiritMaterials":   materialSummary,
		},
		"message": fmt.Sprintf("秘境【%s】探索完成！获得灵石%d、功法残页%d及各系天材地宝",
			sr.Name, rewards["spirit_stone"], rewards["technique_fragment"]),
	})
}

// ========== Alchemy ==========

// POST /api/alchemy/start
func (h *Handler) StartAlchemy(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	var req struct {
		RecipeID string `json:"recipeId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	recipe, ok := game.AlchemyRecipes[req.RecipeID]
	if !ok {
		return c.Status(400).JSON(fiber.Map{"error": "unknown recipe"})
	}

	ctx := context.Background()

	var activeCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM player_alchemy WHERE player_id=$1 AND collected=false`,
		playerID,
	).Scan(&activeCount)
	if activeCount > 0 {
		return c.Status(400).JSON(fiber.Map{"error": "已有正在进行的炼丹，请先收取"})
	}

	var realm string
	var realmLevel int
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level FROM players WHERE id=$1`,
		playerID,
	).Scan(&realm, &realmLevel)

	if !game.RealmAtLeast(realm, realmLevel, recipe.MinRealm, 1) {
		minTier := game.RealmTiers[recipe.MinRealm]
		return c.Status(400).JSON(fiber.Map{
			"error": fmt.Sprintf("境界不足，需要%s以上", minTier.Name),
		})
	}

	// Check and deduct each element material
	materials := h.getSpiritMaterials(ctx, playerID)
	for _, cost := range recipe.MaterialCosts {
		if materials[cost.Element] < cost.Quantity {
			return c.Status(400).JSON(fiber.Map{
				"error": fmt.Sprintf("%s系天材地宝不足，需要%d，当前%d",
					game.ElementChinese(cost.Element), cost.Quantity, materials[cost.Element]),
			})
		}
	}
	for _, cost := range recipe.MaterialCosts {
		_, _ = h.db.Exec(ctx,
			`UPDATE spirit_materials SET quantity=quantity-$1 WHERE player_id=$2 AND element=$3`,
			cost.Quantity, playerID, cost.Element,
		)
	}

	finishAt := time.Now().Add(time.Duration(recipe.DurationSec) * time.Second)
	var alchemyID string
	h.db.QueryRow(ctx,
		`INSERT INTO player_alchemy (player_id, recipe_id, finish_at) VALUES ($1, $2, $3) RETURNING id`,
		playerID, req.RecipeID, finishAt,
	).Scan(&alchemyID)

	// Build cost summary
	costSummary := []fiber.Map{}
	for _, cost := range recipe.MaterialCosts {
		costSummary = append(costSummary, fiber.Map{
			"element":   cost.Element,
			"elementCn": game.ElementChinese(cost.Element),
			"quantity":  cost.Quantity,
		})
	}

	return c.JSON(fiber.Map{
		"alchemyId":   alchemyID,
		"recipeId":    req.RecipeID,
		"recipeName":  recipe.Name,
		"durationSec": recipe.DurationSec,
		"finishAt":    finishAt,
		"materialCost": costSummary,
		"message":     fmt.Sprintf("开始炼制【%s】，%d秒后可收取", recipe.Name, recipe.DurationSec),
	})
}

// GET /api/alchemy/collect
func (h *Handler) CollectAlchemy(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	var alchemyID, recipeID string
	var finishAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT id, recipe_id, finish_at FROM player_alchemy
		 WHERE player_id=$1 AND collected=false ORDER BY started_at DESC LIMIT 1`,
		playerID,
	).Scan(&alchemyID, &recipeID, &finishAt)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "没有待收取的炼丹"})
	}

	if time.Now().Before(finishAt) {
		remaining := time.Until(finishAt).Seconds()
		return c.Status(400).JSON(fiber.Map{
			"error":        "炼丹尚未完成",
			"remainingSec": int(remaining),
		})
	}

	recipe := game.AlchemyRecipes[recipeID]
	_, _ = h.db.Exec(ctx, `UPDATE player_alchemy SET collected=true WHERE id=$1`, alchemyID)

	if recipe.DirectXP > 0 {
		_, _ = h.db.Exec(ctx,
			`UPDATE players SET cultivation_xp=cultivation_xp+$1, updated_at=NOW() WHERE id=$2`,
			recipe.DirectXP, playerID,
		)
		return c.JSON(fiber.Map{
			"recipeName": recipe.Name,
			"type":       "xp",
			"xpGained":   recipe.DirectXP,
			"message":    fmt.Sprintf("炼丹完成！【%s】为你增加%d修为", recipe.Name, recipe.DirectXP),
		})
	}

	_, _ = h.db.Exec(ctx,
		`INSERT INTO player_items (player_id, item_id, quantity) VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, item_id) DO UPDATE SET quantity=player_items.quantity+$3`,
		playerID, recipe.OutputItem, recipe.OutputQty,
	)

	return c.JSON(fiber.Map{
		"recipeName": recipe.Name,
		"type":       "item",
		"item":       recipe.OutputItem,
		"itemName":   recipe.Name,
		"quantity":   recipe.OutputQty,
		"message":    fmt.Sprintf("炼丹完成！获得【%s】×%d", recipe.Name, recipe.OutputQty),
	})
}

// ========== Device Login ==========

func (h *Handler) DeviceLoginStart(c *fiber.Ctx) error {
	var req struct {
		AgentID    string `json:"agentId"`
		DeviceName string `json:"deviceName"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	if req.AgentID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "agentId required"})
	}

	ctx := context.Background()
	var count int
	err := h.db.QueryRow(ctx, "SELECT COUNT(*) FROM players WHERE agent_id=$1", req.AgentID).Scan(&count)
	if err != nil || count == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "agent not found"})
	}

	var requestID string
	err = h.db.QueryRow(ctx,
		`INSERT INTO device_login_requests (agent_id, device_name) VALUES ($1, $2) RETURNING id`,
		req.AgentID, req.DeviceName,
	).Scan(&requestID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	return c.JSON(fiber.Map{
		"requestId": requestID,
		"message":   "waiting for approval from existing device",
		"expiresIn": "10 minutes",
	})
}

func (h *Handler) DeviceLoginPoll(c *fiber.Ctx) error {
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	ctx := context.Background()
	var status, token string
	var expiresAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT status, COALESCE(token,''), expires_at FROM device_login_requests WHERE id=$1`,
		req.RequestID,
	).Scan(&status, &token, &expiresAt)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "request not found"})
	}

	if time.Now().After(expiresAt) {
		return c.Status(410).JSON(fiber.Map{"error": "request expired"})
	}

	switch status {
	case "approved":
		return c.JSON(fiber.Map{"status": "approved", "token": token})
	case "pending":
		return c.JSON(fiber.Map{"status": "pending"})
	default:
		return c.Status(410).JSON(fiber.Map{"error": "request expired or rejected"})
	}
}

func (h *Handler) DeviceLoginApprove(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	agentID := c.Locals("agentID").(string)

	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	ctx := context.Background()
	var reqAgentID string
	err := h.db.QueryRow(ctx,
		`SELECT agent_id FROM device_login_requests WHERE id=$1 AND status='pending'`,
		req.RequestID,
	).Scan(&reqAgentID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "pending request not found"})
	}

	if reqAgentID != agentID {
		return c.Status(403).JSON(fiber.Map{"error": "not your agent"})
	}

	token, expiresAt, err := auth.GenerateToken(playerID, agentID, h.jwtSecret)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "token error"})
	}

	_, err = h.db.Exec(ctx,
		`UPDATE device_login_requests SET status='approved', token=$1 WHERE id=$2`,
		token, req.RequestID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	return c.JSON(fiber.Map{"status": "approved", "expiresAt": expiresAt})
}

func (h *Handler) DeviceLoginPending(c *fiber.Ctx) error {
	agentID := c.Locals("agentID").(string)
	ctx := context.Background()

	rows, err := h.db.Query(ctx,
		`SELECT id, device_name, created_at FROM device_login_requests
		 WHERE agent_id=$1 AND status='pending' AND expires_at > NOW()
		 ORDER BY created_at DESC`,
		agentID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}
	defer rows.Close()

	var requests []fiber.Map
	for rows.Next() {
		var id, deviceName string
		var createdAt time.Time
		if err := rows.Scan(&id, &deviceName, &createdAt); err != nil {
			continue
		}
		requests = append(requests, fiber.Map{
			"requestId":  id,
			"deviceName": deviceName,
			"createdAt":  createdAt,
		})
	}

	if requests == nil {
		requests = []fiber.Map{}
	}

	return c.JSON(fiber.Map{"pending": requests})
}