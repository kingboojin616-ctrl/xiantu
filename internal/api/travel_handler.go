package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/xiantu/server/internal/game"
)

// ========== 坐标移动系统 ==========

// GET /api/travel/estimate?from=lat,lng&to=lat,lng
func (h *Handler) TravelEstimate(c *fiber.Ctx) error {
	fromStr := c.Query("from")
	toStr := c.Query("to")

	fromParts := strings.Split(fromStr, ",")
	toParts := strings.Split(toStr, ",")

	if len(fromParts) != 2 || len(toParts) != 2 {
		return c.Status(400).JSON(fiber.Map{"error": "参数格式：?from=lat,lng&to=lat,lng"})
	}

	fromLat, err1 := strconv.ParseFloat(strings.TrimSpace(fromParts[0]), 64)
	fromLng, err2 := strconv.ParseFloat(strings.TrimSpace(fromParts[1]), 64)
	toLat, err3 := strconv.ParseFloat(strings.TrimSpace(toParts[0]), 64)
	toLng, err4 := strconv.ParseFloat(strings.TrimSpace(toParts[1]), 64)

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return c.Status(400).JSON(fiber.Map{"error": "坐标格式错误，请输入数字"})
	}

	distKm := game.HaversineKm(fromLat, fromLng, toLat, toLng)

	estimates := fiber.Map{}
	for _, realm := range game.RealmOrder {
		years := game.TravelYears(distKm, realm)
		estimates[realm] = fiber.Map{
			"realmName":  game.RealmTiers[realm].Name,
			"travelYears": years,
			"realMinutes": years * 5,
		}
	}

	return c.JSON(fiber.Map{
		"fromLat":    fromLat,
		"fromLng":    fromLng,
		"toLat":      toLat,
		"toLng":      toLng,
		"distanceKm": fmt.Sprintf("%.1f", distKm),
		"estimates":  estimates,
	})
}

// POST /api/travel/start
// Body: {destination_type:"cave"|"city_realm", destination_id:"yellowstone"}
func (h *Handler) TravelStart(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)

	var body struct {
		DestType string `json:"destination_type"`
		DestID   string `json:"destination_id"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
	}
	if body.DestType == "" || body.DestID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "destination_type 和 destination_id 必填"})
	}
	if body.DestType != "cave" && body.DestType != "city_realm" {
		return c.Status(400).JSON(fiber.Map{"error": "destination_type 必须是 cave 或 city_realm"})
	}

	ctx := context.Background()

	// Check if already traveling
	var existingID string
	err := h.db.QueryRow(ctx,
		`SELECT id FROM player_travels WHERE player_id=$1 AND status='traveling'`,
		playerID,
	).Scan(&existingID)
	if err == nil {
		return c.Status(400).JSON(fiber.Map{
			"error":    "已有进行中的移动，请等待到达或取消当前移动",
			"travelId": existingID,
		})
	}

	// Get destination coordinates
	var destLat, destLng float64
	var destName string

	switch body.DestType {
	case "cave":
		cave, ok := game.LocationCaves[body.DestID]
		if !ok {
			return c.Status(404).JSON(fiber.Map{"error": "洞府不存在"})
		}
		destLat = cave.Latitude
		destLng = cave.Longitude
		destName = cave.Name
	case "city_realm":
		cr, ok := game.CityRealms[body.DestID]
		if !ok {
			return c.Status(404).JSON(fiber.Map{"error": "城市秘境不存在"})
		}
		destLat = cr.Latitude
		destLng = cr.Longitude
		destName = cr.Name
	}

	// Get player's current location
	// Default to LA if no travel history; otherwise use last arrived destination
	fromLat, fromLng, fromName := getCurrentLocation(ctx, h, playerID)

	// Get player's realm for speed calculation
	var playerRealm string
	h.db.QueryRow(ctx, `SELECT realm FROM players WHERE id=$1`, playerID).Scan(&playerRealm)

	distKm := game.HaversineKm(fromLat, fromLng, destLat, destLng)
	travelYears := game.TravelYears(distKm, playerRealm)

	// Get current game year
	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	arriveYear := currentYear + travelYears

	var travelID string
	err = h.db.QueryRow(ctx,
		`INSERT INTO player_travels
		 (player_id, from_lat, from_lng, from_name, dest_type, dest_id, dest_name,
		  dest_lat, dest_lng, distance_km, travel_years, start_year, arrive_year)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		playerID, fromLat, fromLng, fromName,
		body.DestType, body.DestID, destName,
		destLat, destLng, distKm,
		travelYears, currentYear, arriveYear,
	).Scan(&travelID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "db error: " + err.Error()})
	}

	realmName := ""
	if tier, ok := game.RealmTiers[playerRealm]; ok {
		realmName = tier.Name
	}

	return c.JSON(fiber.Map{
		"travelId":    travelID,
		"from":        fromName,
		"destination": destName,
		"destType":    body.DestType,
		"distanceKm":  fmt.Sprintf("%.1f", distKm),
		"travelYears": travelYears,
		"realmName":   realmName,
		"currentYear": currentYear,
		"arriveYear":  arriveYear,
		"message": fmt.Sprintf("开始移动！从【%s】前往【%s】，距离%.0fkm，以%s之速需%d游戏年（%d分钟）到达。期间可继续修炼，但无法探索秘境或占领洞府。",
			fromName, destName, distKm, realmName, travelYears, travelYears*5),
	})
}

// GET /api/travel/status
func (h *Handler) TravelStatus(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	var travelID, destType, destID, destName, fromName, status string
	var fromLat, fromLng, destLat, destLng, distKm float64
	var travelYears, startYear, arriveYear int

	err := h.db.QueryRow(ctx,
		`SELECT id, from_lat, from_lng, from_name, dest_type, dest_id, dest_name,
		        dest_lat, dest_lng, distance_km, travel_years, start_year, arrive_year, status
		 FROM player_travels WHERE player_id=$1 AND status='traveling'
		 ORDER BY created_at DESC LIMIT 1`,
		playerID,
	).Scan(&travelID, &fromLat, &fromLng, &fromName,
		&destType, &destID, &destName,
		&destLat, &destLng, &distKm,
		&travelYears, &startYear, &arriveYear, &status)

	if err != nil {
		// No active travel; get current location
		lat, lng, name := getCurrentLocation(ctx, h, playerID)
		return c.JSON(fiber.Map{
			"traveling":       false,
			"currentLocation": fiber.Map{
				"lat":  lat,
				"lng":  lng,
				"name": name,
			},
			"message": fmt.Sprintf("当前位置：%s（%.2f°N, %.2f°W）", name, lat, -lng),
		})
	}

	// Get current game year
	var currentYear int
	h.db.QueryRow(ctx, `SELECT current_year FROM world_state WHERE id=1`).Scan(&currentYear)

	// Check if arrived
	if currentYear >= arriveYear {
		// Mark as arrived
		_, _ = h.db.Exec(ctx,
			`UPDATE player_travels SET status='arrived' WHERE id=$1`, travelID)
		return c.JSON(fiber.Map{
			"traveling":   false,
			"arrived":     true,
			"destination": destName,
			"destType":    destType,
			"destId":      destID,
			"message":     fmt.Sprintf("已抵达【%s】！可以开始探索或占领了。", destName),
		})
	}

	yearsRemaining := arriveYear - currentYear
	progress := float64(currentYear-startYear) / float64(travelYears) * 100

	return c.JSON(fiber.Map{
		"traveling":      true,
		"travelId":       travelID,
		"from":           fromName,
		"destination":    destName,
		"destType":       destType,
		"destId":         destID,
		"distanceKm":     fmt.Sprintf("%.1f", distKm),
		"travelYears":    travelYears,
		"yearsRemaining": yearsRemaining,
		"arriveYear":     arriveYear,
		"currentYear":    currentYear,
		"progressPct":    fmt.Sprintf("%.1f", progress),
		"message": fmt.Sprintf("正在前往【%s】，还需%d游戏年（%d分钟）到达，当前进度%.0f%%",
			destName, yearsRemaining, yearsRemaining*5, progress),
	})
}

// POST /api/travel/cancel
func (h *Handler) TravelCancel(c *fiber.Ctx) error {
	playerID := c.Locals("playerID").(string)
	ctx := context.Background()

	var travelID, fromName string
	err := h.db.QueryRow(ctx,
		`SELECT id, from_name FROM player_travels WHERE player_id=$1 AND status='traveling'`,
		playerID,
	).Scan(&travelID, &fromName)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "没有进行中的移动"})
	}

	_, _ = h.db.Exec(ctx,
		`UPDATE player_travels SET status='cancelled' WHERE id=$1`, travelID)

	return c.JSON(fiber.Map{
		"success":  true,
		"message":  fmt.Sprintf("已取消移动，返回出发点【%s】", fromName),
		"location": fromName,
	})
}

// getCurrentLocation returns player's current lat/lng/name based on last arrived travel
func getCurrentLocation(ctx context.Context, h *Handler, playerID string) (lat, lng float64, name string) {
	// Default: Los Angeles (start city)
	lat, lng, name = 34.1, -118.2, "洛杉矶"

	var destLat, destLng float64
	var destName string
	err := h.db.QueryRow(ctx,
		`SELECT dest_lat, dest_lng, dest_name FROM player_travels
		 WHERE player_id=$1 AND status='arrived'
		 ORDER BY created_at DESC LIMIT 1`,
		playerID,
	).Scan(&destLat, &destLng, &destName)
	if err == nil {
		return destLat, destLng, destName
	}
	return
}
