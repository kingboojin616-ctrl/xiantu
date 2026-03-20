package api

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// ========== 随机事件系统 ==========

// GET /api/events/recent  最近发生的事件列表（全服可见）
func (h *Handler) EventsRecent(c *fiber.Ctx) error {
	ctx := context.Background()

	limit := 20
	rows, err := h.db.Query(ctx,
		`SELECT le.id, le.player_id, p.username, le.location_type, le.location_id,
		        le.location_name, le.encounter_type, le.element, le.event_seed,
		        le.game_year, le.created_at
		 FROM location_events le
		 JOIN players p ON p.id = le.player_id
		 ORDER BY le.created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}
	defer rows.Close()

	var events []fiber.Map
	for rows.Next() {
		var id, playerID, username, locType, locID, locName, encType, element string
		var gameYear int
		var eventSeedRaw json.RawMessage
		var createdAt interface{}

		if err := rows.Scan(&id, &playerID, &username, &locType, &locID, &locName,
			&encType, &element, &eventSeedRaw, &gameYear, &createdAt); err != nil {
			continue
		}

		var seed interface{}
		json.Unmarshal(eventSeedRaw, &seed)

		events = append(events, fiber.Map{
			"id":           id,
			"username":     username,
			"locationType": locType,
			"locationId":   locID,
			"locationName": locName,
			"encounterType": encType,
			"element":      element,
			"eventSeed":    seed,
			"gameYear":     gameYear,
			"createdAt":    createdAt,
		})
	}

	if events == nil {
		events = []fiber.Map{}
	}

	return c.JSON(fiber.Map{"events": events, "total": len(events)})
}

// GET /api/events/my  我的事件历史
func (h *Handler) EventsMy(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	rows, err := h.db.Query(ctx,
		`SELECT id, location_type, location_id, location_name, encounter_type,
		        element, event_seed, game_year, created_at
		 FROM location_events
		 WHERE player_id=$1
		 ORDER BY created_at DESC
		 LIMIT 50`,
		playerID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error"})
	}
	defer rows.Close()

	var events []fiber.Map
	for rows.Next() {
		var id, locType, locID, locName, encType, element string
		var gameYear int
		var eventSeedRaw json.RawMessage
		var createdAt interface{}

		if err := rows.Scan(&id, &locType, &locID, &locName, &encType,
			&element, &eventSeedRaw, &gameYear, &createdAt); err != nil {
			continue
		}

		var seed interface{}
		json.Unmarshal(eventSeedRaw, &seed)

		events = append(events, fiber.Map{
			"id":            id,
			"locationType":  locType,
			"locationId":    locID,
			"locationName":  locName,
			"encounterType": encType,
			"element":       element,
			"eventSeed":     seed,
			"gameYear":      gameYear,
			"createdAt":     createdAt,
		})
	}

	if events == nil {
		events = []fiber.Map{}
	}

	return c.JSON(fiber.Map{"events": events, "total": len(events)})
}
