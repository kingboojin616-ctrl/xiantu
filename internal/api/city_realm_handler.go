package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xiantu/server/internal/game"
)

// ========== 城市秘境系统（30个美国城市） ==========

// GET /api/secret-realms  (override to include city realms, keep backward compat)
// We add a new route GET /api/city-realms for the new city-based system

// GET /api/city-realms
func (h *Handler) CityRealmList(c *fiber.Ctx) error {
	playerID := ""
	if pid := c.Locals("playerID"); pid != nil {
		playerID = pid.(string)
	}

	// Get active explorations for this player
	activeCities := map[string]bool{}
	if playerID != "" {
		ctx := context.Background()
		rows, _ := h.db.Query(ctx,
			`SELECT city_id FROM city_realm_explorations WHERE player_id=$1 AND collected=false`,
			playerID,
		)
		if rows != nil {
			for rows.Next() {
				var cid string
				rows.Scan(&cid)
				activeCities[cid] = true
			}
			rows.Close()
		}
	}

	var realms []fiber.Map
	for _, id := range game.CityRealmOrder {
		cr := game.CityRealms[id]
		entry := fiber.Map{
			"id":          cr.ID,
			"name":        cr.Name,
			"nameEn":      cr.NameEn,
			"elements":    cr.Elements,
			"elementsCn":  elemListCn(cr.Elements),
			"durationSec": cr.DurationSec,
			"soulCost":    cr.SoulCost,
			"baseRewards": fiber.Map{
				"cultivationXp": cr.BaseXP,
				"spiritStone":   cr.BaseSpiritStone,
				"materialsPerElement": cr.BaseMaterials,
			},
			"desc":         cr.Desc,
			"isExploring":  activeCities[id],
		}
		realms = append(realms, entry)
	}

	return c.JSON(fiber.Map{"cityRealms": realms, "total": len(realms)})
}

func elemListCn(elems []string) []string {
	out := make([]string, len(elems))
	for i, e := range elems {
		out[i] = game.ElementChinese(e)
	}
	return out
}

// POST /api/city-realms/:id/enter  进入秘境
func (h *Handler) CityRealmEnter(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	cityID := c.Params("id")

	cr, ok := game.CityRealms[cityID]
	if !ok {
		return c.Status(404).JSON(fiber.Map{"error": "城市秘境不存在"})
	}

	ctx := context.Background()

	// Check no active city realm exploration
	var activeCount int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM city_realm_explorations WHERE player_id=$1 AND collected=false`,
		playerID,
	).Scan(&activeCount)
	if activeCount > 0 {
		return c.Status(400).JSON(fiber.Map{"error": "已有进行中的城市秘境探索，请先结算"})
	}

	// Check soul sense
	var soulSense int64
	h.db.QueryRow(ctx, `SELECT soul_sense FROM players WHERE id=$1`, playerID).Scan(&soulSense)

	if soulSense < cr.SoulCost {
		return c.Status(400).JSON(fiber.Map{
			"error": fmt.Sprintf("神识值不足，需要%d，当前%d", cr.SoulCost, soulSense),
		})
	}

	// Deduct soul sense
	_, _ = h.db.Exec(ctx,
		`UPDATE players SET soul_sense=soul_sense-$1, updated_at=NOW() WHERE id=$2`,
		cr.SoulCost, playerID,
	)

	// Pre-generate narrative seed and rewards
	rewards, narrativeSeed := game.RollCityRealmRewards(cr)
	seedJSON, _ := json.Marshal(narrativeSeed)

	finishAt := time.Now().Add(time.Duration(cr.DurationSec) * time.Second)
	var exploID string
	err := h.db.QueryRow(ctx,
		`INSERT INTO city_realm_explorations (player_id, city_id, finish_at, narrative_seed)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		playerID, cityID, finishAt, seedJSON,
	).Scan(&exploID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}

	// Store rewards temporarily in narrative seed for retrieval
	_ = rewards

	return c.JSON(fiber.Map{
		"explorationId": exploID,
		"cityId":        cityID,
		"cityName":      cr.Name,
		"durationSec":   cr.DurationSec,
		"finishAt":      finishAt,
		"soulCostPaid":  cr.SoulCost,
		"narrativeSeed": narrativeSeed,
		"message": fmt.Sprintf("踏入【%s】秘境！神识消耗%d，%d秒后可结算收益。%s",
			cr.Name, cr.SoulCost, cr.DurationSec,
			narrativeSeed["narrative_hint"]),
	})
}

// GET /api/city-realms/:id/status  探索进度
func (h *Handler) CityRealmStatus(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	cityID := c.Params("id")

	ctx := context.Background()

	var exploID string
	var finishAt time.Time
	var collected bool
	var narrativeSeed json.RawMessage
	err := h.db.QueryRow(ctx,
		`SELECT id, finish_at, collected, narrative_seed
		 FROM city_realm_explorations
		 WHERE player_id=$1 AND city_id=$2
		 ORDER BY started_at DESC LIMIT 1`,
		playerID, cityID,
	).Scan(&exploID, &finishAt, &collected, &narrativeSeed)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "未找到此城市的探索记录"})
	}

	remaining := time.Until(finishAt).Seconds()
	if remaining < 0 {
		remaining = 0
	}

	var seed interface{}
	json.Unmarshal(narrativeSeed, &seed)

	return c.JSON(fiber.Map{
		"explorationId":  exploID,
		"cityId":         cityID,
		"cityName":       game.CityRealms[cityID].Name,
		"finishAt":       finishAt,
		"remainingSec":   int(remaining),
		"completed":      remaining == 0,
		"collected":      collected,
		"narrativeSeed":  seed,
	})
}

// POST /api/city-realms/:id/exit  提前离开/结算
func (h *Handler) CityRealmExit(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	cityID := c.Params("id")

	ctx := context.Background()

	var exploID string
	var finishAt time.Time
	var collected bool
	var narrativeSeedRaw json.RawMessage
	err := h.db.QueryRow(ctx,
		`SELECT id, finish_at, collected, narrative_seed
		 FROM city_realm_explorations
		 WHERE player_id=$1 AND city_id=$2 AND collected=false
		 ORDER BY started_at DESC LIMIT 1`,
		playerID, cityID,
	).Scan(&exploID, &finishAt, &collected, &narrativeSeedRaw)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "未找到进行中的秘境探索"})
	}

	cr := game.CityRealms[cityID]
	now := time.Now()
	isEarly := now.Before(finishAt)

	// Calculate completion ratio (for early exit penalty)
	elapsed := now.Sub(finishAt.Add(-time.Duration(cr.DurationSec) * time.Second))
	completionRatio := float64(elapsed.Seconds()) / float64(cr.DurationSec)
	if completionRatio > 1 {
		completionRatio = 1
	}
	if completionRatio < 0 {
		completionRatio = 0
	}

	// Re-roll rewards based on completion (early exit gets proportional rewards)
	rewards, narrativeSeed := game.RollCityRealmRewards(cr)
	if isEarly && completionRatio < 1 {
		for k, v := range rewards {
			rewards[k] = int64(float64(v) * completionRatio)
		}
		narrativeSeed["narrative_hint"] = fmt.Sprintf("（提前撤离，仅获得%.0f%%收益）%s", completionRatio*100, narrativeSeed["narrative_hint"])
	}

	// Apply cultivation XP
	if xp, ok := rewards["cultivation_xp"]; ok && xp > 0 {
		_, _ = h.db.Exec(ctx,
			`UPDATE players SET cultivation_xp=cultivation_xp+$1, updated_at=NOW() WHERE id=$2`,
			xp, playerID,
		)
	}
	// Apply spirit stone
	if stone, ok := rewards["spirit_stone"]; ok && stone > 0 {
		_, _ = h.db.Exec(ctx,
			`UPDATE players SET spirit_stone=spirit_stone+$1, updated_at=NOW() WHERE id=$2`,
			stone, playerID,
		)
	}
	// Apply materials
	materialSummary := map[string]int64{}
	for _, elem := range game.Elements {
		key := "material_" + elem
		if qty, ok := rewards[key]; ok && qty > 0 {
			_, _ = h.db.Exec(ctx,
				`INSERT INTO spirit_materials (player_id, element, quantity) VALUES ($1, $2, $3)
				 ON CONFLICT (player_id, element) DO UPDATE SET quantity=spirit_materials.quantity+$3`,
				playerID, elem, qty,
			)
			materialSummary[elem] = qty
		}
	}

	// Mark collected
	_, _ = h.db.Exec(ctx, `UPDATE city_realm_explorations SET collected=true WHERE id=$1`, exploID)

	// Publish realm complete event
	h.engine.PublishRealmCompleteEvent(ctx, cityID, playerID)

	// Generate extended location event seed with encounter type
	var playerRealm string
	h.db.QueryRow(ctx, `SELECT realm FROM players WHERE id=$1`, playerID).Scan(&playerRealm)

	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	extSeed := game.GenerateCityRealmEventSeed(cityID, playerRealm, cr.DurationSec)
	if extSeed != nil {
		encType, _ := extSeed["encounter_type"].(string)
		element := ""
		if len(cr.Elements) > 0 {
			element = cr.Elements[0]
		}
		h.engine.SaveAndPublishLocationEvent(ctx,
			playerID, "city_realm", cityID, cr.Name,
			encType, element, extSeed, currentYear,
		)
	}

	hint := ""
	if h, ok := narrativeSeed["narrative_hint"].(string); ok {
		hint = h
	}

	return c.JSON(fiber.Map{
		"cityId":          cityID,
		"cityName":        cr.Name,
		"earlyExit":       isEarly,
		"completionPct":   int(completionRatio * 100),
		"rewards": fiber.Map{
			"cultivationXp": rewards["cultivation_xp"],
			"spiritStone":   rewards["spirit_stone"],
			"spiritMaterials": materialSummary,
		},
		"narrativeSeed":   narrativeSeed,
		"locationEvent":   extSeed,
		"narrativeHint":   hint,
		"message": fmt.Sprintf("【%s】秘境探索结算！获得修为%d、灵石%d。%s",
			cr.Name, rewards["cultivation_xp"], rewards["spirit_stone"], hint),
	})
}
