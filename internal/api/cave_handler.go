package api

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xiantu/server/internal/game"
)

// ========== 洞府系统（美国景点） ==========

// GET /api/caves
func (h *Handler) CaveList(c *fiber.Ctx) error {
	ctx := context.Background()

	// Get all occupations
	rows, err := h.db.Query(ctx,
		`SELECT co.cave_id, p.username, p.realm, p.realm_level, co.occupied_at
		 FROM cave_occupations co
		 JOIN players p ON p.id = co.player_id`,
	)
	occupations := map[string]fiber.Map{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var caveID, username, realm string
			var realmLevel int
			var occupiedAt time.Time
			rows.Scan(&caveID, &username, &realm, &realmLevel, &occupiedAt)
			occupations[caveID] = fiber.Map{
				"username":  username,
				"realmName": game.RealmDisplayName(realm, realmLevel),
				"occupiedAt": occupiedAt,
			}
		}
		rows.Close()
	}

	var caves []fiber.Map
	for _, id := range game.CaveOrder {
		cave := game.LocationCaves[id]
		entry := fiber.Map{
			"id":         cave.ID,
			"name":       cave.Name,
			"nameEn":     cave.NameEn,
			"element":    cave.Element,
			"elementCn":  game.ElementChinese(cave.Element),
			"bonusType":  string(cave.BonusType),
			"bonusValue": cave.BonusValue,
			"desc":       cave.Desc,
			"occupied":   false,
			"occupant":   nil,
		}
		if occ, ok := occupations[id]; ok {
			entry["occupied"] = true
			entry["occupant"] = occ
		}
		caves = append(caves, entry)
	}

	return c.JSON(fiber.Map{"caves": caves, "total": len(caves)})
}

// GET /api/caves/:id
func (h *Handler) CaveDetail(c *fiber.Ctx) error {
	caveID := c.Params("id")
	cave, ok := game.LocationCaves[caveID]
	if !ok {
		return c.Status(404).JSON(fiber.Map{"error": "洞府不存在"})
	}

	ctx := context.Background()

	var occupantID, username, realm string
	var realmLevel int
	var occupiedAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT co.player_id, p.username, p.realm, p.realm_level, co.occupied_at
		 FROM cave_occupations co
		 JOIN players p ON p.id = co.player_id
		 WHERE co.cave_id = $1`,
		caveID,
	).Scan(&occupantID, &username, &realm, &realmLevel, &occupiedAt)

	cultivPct, stonePct, matPct, bkPct := game.CaveYearlyReward(cave)

	result := fiber.Map{
		"id":         cave.ID,
		"name":       cave.Name,
		"nameEn":     cave.NameEn,
		"element":    cave.Element,
		"elementCn":  game.ElementChinese(cave.Element),
		"bonusType":  string(cave.BonusType),
		"bonusValue": cave.BonusValue,
		"desc":       cave.Desc,
		"rewards": fiber.Map{
			"cultivationPctPerYear":   cultivPct,
			"spiritStoneFlatPerYear":  stonePct,
			"materialPctPerYear":      matPct,
			"breakthroughBonusPct":    bkPct,
		},
		"occupied": err == nil,
	}
	if err == nil {
		result["occupant"] = fiber.Map{
			"playerId":   occupantID,
			"username":   username,
			"realmName":  game.RealmDisplayName(realm, realmLevel),
			"occupiedAt": occupiedAt,
		}
	}
	return c.JSON(result)
}

// POST /api/caves/:id/claim  占领洞府（无人占领时）
func (h *Handler) CaveClaim(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	caveID := c.Params("id")

	cave, ok := game.LocationCaves[caveID]
	if !ok {
		return c.Status(404).JSON(fiber.Map{"error": "洞府不存在"})
	}

	ctx := context.Background()

	// Check if already occupied
	var existingPlayerID string
	err := h.db.QueryRow(ctx,
		`SELECT player_id FROM cave_occupations WHERE cave_id = $1`,
		caveID,
	).Scan(&existingPlayerID)

	if err == nil {
		if existingPlayerID == playerID {
			return c.Status(400).JSON(fiber.Map{"error": "你已经占领了此洞府"})
		}
		return c.Status(409).JSON(fiber.Map{
			"error":  "此洞府已被他人占领，请使用挑战接口",
			"caveId": caveID,
		})
	}

	// Get current world year
	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	_, err = h.db.Exec(ctx,
		`INSERT INTO cave_occupations (cave_id, player_id, last_reward_year)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (cave_id) DO UPDATE SET player_id=$2, occupied_at=NOW(), last_reward_year=$3`,
		caveID, playerID, currentYear,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	// Publish event
	h.engine.PublishCaveEvent(ctx, "cave_claimed", caveID, playerID)

	return c.JSON(fiber.Map{
		"success":   true,
		"caveId":    caveID,
		"caveName":  cave.Name,
		"element":   game.ElementChinese(cave.Element),
		"bonusType": string(cave.BonusType),
		"bonusValue": cave.BonusValue,
		"message":   fmt.Sprintf("成功占领【%s】！每游戏年将获得额外收益：%s +%d%%", cave.Name, cave.BonusType, cave.BonusValue),
	})
}

// POST /api/caves/:id/challenge  挑战占领者
func (h *Handler) CaveChallenge(c *fiber.Ctx) error {
	challengerID := c.Locals("playerID").(string)
	caveID := c.Params("id")

	cave, ok := game.LocationCaves[caveID]
	if !ok {
		return c.Status(404).JSON(fiber.Map{"error": "洞府不存在"})
	}

	ctx := context.Background()

	// Get occupant
	var defenderID string
	err := h.db.QueryRow(ctx,
		`SELECT player_id FROM cave_occupations WHERE cave_id = $1`,
		caveID,
	).Scan(&defenderID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "此洞府无人占领，请直接使用 claim 接口"})
	}

	if defenderID == challengerID {
		return c.Status(400).JSON(fiber.Map{"error": "无法挑战自己"})
	}

	// Get challenger stats
	var challRealm, challUsername string
	var challLevel int
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level, username FROM players WHERE id=$1`,
		challengerID,
	).Scan(&challRealm, &challLevel, &challUsername)

	// Get defender stats
	var defRealm, defUsername string
	var defLevel int
	h.db.QueryRow(ctx,
		`SELECT realm, realm_level, username FROM players WHERE id=$1`,
		defenderID,
	).Scan(&defRealm, &defLevel, &defUsername)

	challOrder := game.RealmTiers[challRealm].Order
	defOrder := game.RealmTiers[defRealm].Order

	// Battle resolution: higher realm wins; same realm = random
	var challengerWins bool
	var battleDesc string
	if challOrder > defOrder {
		challengerWins = true
		battleDesc = fmt.Sprintf("挑战者【%s】境界%s高于守护者【%s】境界%s，势如破竹！",
			challUsername, game.RealmDisplayName(challRealm, challLevel),
			defUsername, game.RealmDisplayName(defRealm, defLevel))
	} else if challOrder < defOrder {
		challengerWins = false
		battleDesc = fmt.Sprintf("守护者【%s】境界%s高于挑战者【%s】境界%s，成功守护！",
			defUsername, game.RealmDisplayName(defRealm, defLevel),
			challUsername, game.RealmDisplayName(challRealm, challLevel))
	} else {
		// Same major realm: compare level
		if challLevel > defLevel {
			challengerWins = true
			battleDesc = fmt.Sprintf("同境界对决，挑战者【%s】%s小境界略高，险胜！",
				challUsername, game.RealmDisplayName(challRealm, challLevel))
		} else if challLevel < defLevel {
			challengerWins = false
			battleDesc = fmt.Sprintf("同境界对决，守护者【%s】%s小境界略高，守住了！",
				defUsername, game.RealmDisplayName(defRealm, defLevel))
		} else {
			// Exact same realm: 50/50
			challengerWins = rand.Intn(2) == 0
			if challengerWins {
				battleDesc = fmt.Sprintf("势均力敌！随机天命眷顾了挑战者【%s】！", challUsername)
			} else {
				battleDesc = fmt.Sprintf("势均力敌！随机天命眷顾了守护者【%s】！", defUsername)
			}
		}
	}

	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	if challengerWins {
		_, _ = h.db.Exec(ctx,
			`UPDATE cave_occupations SET player_id=$1, occupied_at=NOW(), last_reward_year=$2 WHERE cave_id=$3`,
			challengerID, currentYear, caveID,
		)
		h.engine.PublishCaveEvent(ctx, "cave_challenged", caveID, challengerID)

		// Generate location event for the defender (被驱逐时触发)
		generateCaveLeaveEvent(ctx, h, defenderID, caveID, currentYear)

		return c.JSON(fiber.Map{
			"success":         true,
			"challengerWins":  true,
			"caveId":          caveID,
			"caveName":        cave.Name,
			"battleDesc":      battleDesc,
			"newOccupant":     challUsername,
			"prevOccupant":    defUsername,
			"message":         fmt.Sprintf("挑战成功！【%s】已落入你手！%s", cave.Name, battleDesc),
		})
	}

	return c.JSON(fiber.Map{
		"success":        true,
		"challengerWins": false,
		"caveId":         caveID,
		"caveName":       cave.Name,
		"battleDesc":     battleDesc,
		"occupant":       defUsername,
		"message":        fmt.Sprintf("挑战失败！【%s】守住了！%s", cave.Name, battleDesc),
	})
}

// POST /api/caves/:id/leave  主动离开洞府（触发随机事件）
func (h *Handler) CaveLeave(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	caveID := c.Params("id")

	cave, ok := game.LocationCaves[caveID]
	if !ok {
		return c.Status(404).JSON(fiber.Map{"error": "洞府不存在"})
	}

	ctx := context.Background()

	// Verify player occupies this cave and get occupation time
	var occupantID string
	var occupiedAt time.Time
	err := h.db.QueryRow(ctx,
		`SELECT player_id, occupied_at FROM cave_occupations WHERE cave_id=$1`,
		caveID,
	).Scan(&occupantID, &occupiedAt)
	if err != nil || occupantID != playerID {
		return c.Status(400).JSON(fiber.Map{"error": "你未占领此洞府"})
	}

	// Remove occupation
	_, _ = h.db.Exec(ctx, `DELETE FROM cave_occupations WHERE cave_id=$1`, caveID)
	h.engine.PublishCaveEvent(ctx, "cave_vacated", caveID, playerID)

	// Generate and return location event
	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	yearsOccupied := int(time.Since(occupiedAt).Minutes()/5) + 1
	eventSeed := generateCaveLeaveEventWithYears(ctx, h, playerID, caveID, currentYear, yearsOccupied)

	return c.JSON(fiber.Map{
		"success":   true,
		"caveId":    caveID,
		"caveName":  cave.Name,
		"eventSeed": eventSeed,
		"message":   fmt.Sprintf("已离开【%s】！离开时触发随机事件...", cave.Name),
	})
}

// generateCaveLeaveEvent generates and saves a location event when player leaves a cave (defender evicted)
func generateCaveLeaveEvent(ctx context.Context, h *Handler, playerID, caveID string, currentYear int) map[string]interface{} {
	return generateCaveLeaveEventWithYears(ctx, h, playerID, caveID, currentYear, 1)
}

// generateCaveLeaveEventWithYears generates a cave leave event with known years occupied
func generateCaveLeaveEventWithYears(ctx context.Context, h *Handler, playerID, caveID string, currentYear, yearsOccupied int) map[string]interface{} {
	cave, ok := game.LocationCaves[caveID]
	if !ok {
		return nil
	}

	// Get player realm
	var playerRealm string
	h.db.QueryRow(ctx, `SELECT realm FROM players WHERE id=$1`, playerID).Scan(&playerRealm)

	seed := game.GenerateCaveEventSeed(caveID, playerRealm, yearsOccupied)
	if seed == nil {
		return nil
	}

	encounterType, _ := seed["encounter_type"].(string)

	// Save event to DB and publish WS push
	h.engine.SaveAndPublishLocationEvent(ctx,
		playerID, "cave", caveID, cave.Name,
		encounterType, cave.Element, seed, currentYear,
	)

	return seed
}


