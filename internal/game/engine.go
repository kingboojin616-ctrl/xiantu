package game

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Engine struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

func NewEngine(db *pgxpool.Pool, rdb *redis.Client) *Engine {
	return &Engine{db: db, rdb: rdb}
}

// Run advances one game year every 5 minutes
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(GameYearDuration)
	defer ticker.Stop()

	log.Println("⏱️  Game engine started (1 game year = 5 real minutes)")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.processYear(ctx); err != nil {
				log.Printf("Year processing error: %v", err)
			}
		}
	}
}

// GetCurrentYear returns the current game year from DB
func (e *Engine) GetCurrentYear(ctx context.Context) (int, error) {
	var year int
	err := e.db.QueryRow(ctx, "SELECT current_year FROM world_state WHERE id=1").Scan(&year)
	return year, err
}

// GetWorldStatus returns full world status
func (e *Engine) GetWorldStatus(ctx context.Context) (map[string]interface{}, error) {
	var currentYear int
	var worldStartedAt time.Time
	err := e.db.QueryRow(ctx,
		"SELECT current_year, world_started_at FROM world_state WHERE id=1",
	).Scan(&currentYear, &worldStartedAt)
	if err != nil {
		return nil, err
	}

	nextYear, nextElement := NextTribulationYear(currentYear + 1)
	yearsToNext := nextYear - currentYear

	result := map[string]interface{}{
		"currentYear":          currentYear,
		"worldStartedAt":       worldStartedAt,
		"nextTribulationYear":  nextYear,
		"yearsToTribulation":   yearsToNext,
		"nextTribulationElement": nextElement,
		"nextTribulationElementCn": ElementChinese(nextElement),
		"realSecondsPerYear":   GameYearDuration.Seconds(),
		"realMinutesPerYear":   GameYearDuration.Minutes(),
	}

	// Check active tribulation
	var tribID, tribElement, tribStatus string
	var windowEnd time.Time
	err = e.db.QueryRow(ctx,
		`SELECT id, element, status, window_end_at FROM tribulation_events
		 WHERE status='active' ORDER BY year DESC LIMIT 1`,
	).Scan(&tribID, &tribElement, &tribStatus, &windowEnd)
	if err == nil {
		remaining := time.Until(windowEnd).Seconds()
		if remaining > 0 {
			result["activeTribulation"] = map[string]interface{}{
				"id":             tribID,
				"element":        tribElement,
				"elementCn":      ElementChinese(tribElement),
				"status":         tribStatus,
				"windowEndAt":    windowEnd,
				"remainingSec":   int(remaining),
			}
		}
	}

	return result, nil
}

func (e *Engine) processYear(ctx context.Context) error {
	// 1. Increment year
	var currentYear int
	err := e.db.QueryRow(ctx,
		`UPDATE world_state SET current_year=current_year+1, last_year_at=NOW()
		 WHERE id=1 RETURNING current_year`,
	).Scan(&currentYear)
	if err != nil {
		return fmt.Errorf("increment year: %w", err)
	}
	log.Printf("📅 Game Year %d", currentYear)

	// 2. Advance cultivation for all active players
	if err := e.advanceCultivation(ctx); err != nil {
		log.Printf("advance cultivation error: %v", err)
	}

	// 3. Recover soul sense for all players
	if err := e.recoverSoulSense(ctx); err != nil {
		log.Printf("recover soul sense error: %v", err)
	}

	// 4. Check if tribulation should start
	if err := e.checkTribulation(ctx, currentYear); err != nil {
		log.Printf("check tribulation error: %v", err)
	}

	// 5. Check if any active tribulation has timed out
	if err := e.checkTribulationTimeout(ctx); err != nil {
		log.Printf("tribulation timeout check error: %v", err)
	}

	// 6. Process cave yearly rewards
	if err := e.processCaveRewards(ctx, currentYear); err != nil {
		log.Printf("cave rewards error: %v", err)
	}

	// 7. Publish year event to Redis
	e.rdb.Publish(ctx, "game:year", fmt.Sprintf("%d", currentYear))

	return nil
}

func (e *Engine) advanceCultivation(ctx context.Context) error {
	rows, err := e.db.Query(ctx,
		`SELECT p.id, p.cultivation_xp, p.spirit_root_multiplier, p.realm, p.realm_level,
		        p.race, p.cave_level, p.equipped_technique
		 FROM players p WHERE p.is_cultivating=true AND p.joined_epoch=true`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type cultivator struct {
		id                string
		xp                int64
		rootMultiplier    float64
		realm             string
		realmLevel        int
		race              string
		caveLevel         int
		equippedTechnique string
	}

	var cultivators []cultivator
	for rows.Next() {
		var c cultivator
		if err := rows.Scan(&c.id, &c.xp, &c.rootMultiplier, &c.realm, &c.realmLevel,
			&c.race, &c.caveLevel, &c.equippedTechnique); err != nil {
			continue
		}
		cultivators = append(cultivators, c)
	}
	rows.Close()

	for _, c := range cultivators {
		xpGain := CalcXPPerYear(c.rootMultiplier, c.race, c.caveLevel, c.equippedTechnique)
		_, _ = e.db.Exec(ctx,
			`UPDATE players SET cultivation_xp=cultivation_xp+$1, updated_at=NOW() WHERE id=$2`,
			xpGain, c.id,
		)
	}

	return nil
}

func (e *Engine) recoverSoulSense(ctx context.Context) error {
	_, err := e.db.Exec(ctx,
		`UPDATE players SET soul_sense = LEAST(soul_sense + $1, soul_sense_max), updated_at=NOW()
		 WHERE joined_epoch=true AND soul_sense < soul_sense_max`,
		SoulSenseRecoveryPerYear,
	)
	return err
}

func (e *Engine) checkTribulation(ctx context.Context, year int) error {
	sched := GetTribulationSchedule(year)
	if sched == nil {
		return nil
	}

	// Check if not already created
	var count int
	err := e.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM tribulation_events WHERE year=$1`, year,
	).Scan(&count)
	if err != nil || count > 0 {
		return err
	}

	// Create tribulation event
	windowEnd := time.Now().Add(time.Duration(sched.WindowHours) * time.Hour)
	_, err = e.db.Exec(ctx,
		`INSERT INTO tribulation_events
		 (year, element, status, window_start_at, window_end_at,
		  req_cultivator_realm, req_cultivator_level, req_cultivator_count,
		  req_spirit_stone, req_material_ratio)
		 VALUES ($1, $2, 'active', NOW(), $3, $4, $5, $6, $7, $8)`,
		year, sched.Element, windowEnd,
		sched.ReqCultivatorRealm, sched.ReqCultivatorLevel, sched.ReqCultivatorCount,
		sched.ReqSpiritStone, sched.ReqMaterialRatio,
	)
	if err != nil {
		return fmt.Errorf("create tribulation event: %w", err)
	}

	log.Printf("⚡ 天劫降临！第%d年 %s系天劫", year, ElementChinese(sched.Element))
	e.rdb.Publish(ctx, "game:tribulation", fmt.Sprintf("year:%d,element:%s", year, sched.Element))
	return nil
}

func (e *Engine) checkTribulationTimeout(ctx context.Context) error {
	// Find active tribulations that have timed out
	rows, err := e.db.Query(ctx,
		`SELECT id FROM tribulation_events WHERE status='active' AND window_end_at < NOW()`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var timedOutIDs []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		timedOutIDs = append(timedOutIDs, id)
	}
	rows.Close()

	for _, id := range timedOutIDs {
		// Mark as failed
		_, _ = e.db.Exec(ctx, `UPDATE tribulation_events SET status='failed' WHERE id=$1`, id)
		log.Printf("💀 天劫失败！ID: %s, 全服重置！", id)

		// Full server reset
		if err := e.resetServer(ctx); err != nil {
			log.Printf("server reset error: %v", err)
		}

		e.rdb.Publish(ctx, "game:reset", id)
	}
	return nil
}

// resetServer resets all players and world state on tribulation failure
func (e *Engine) resetServer(ctx context.Context) error {
	log.Println("🔄 全服重置中...")

	// Reset all players
	_, err := e.db.Exec(ctx, `
		UPDATE players SET
			realm='qi_refining', realm_level=1, cultivation_xp=0,
			spirit_stone=100, soul_sense=100,
			is_cultivating=false, joined_epoch=false,
			last_offline_claim=NOW(), updated_at=NOW()
	`)
	if err != nil {
		return fmt.Errorf("reset players: %w", err)
	}

	// Reset all spirit materials
	_, _ = e.db.Exec(ctx, `DELETE FROM spirit_materials`)

	// Reset player items (breakthrough pills etc.)
	_, _ = e.db.Exec(ctx, `DELETE FROM player_items`)

	// Reset world year
	_, _ = e.db.Exec(ctx, `UPDATE world_state SET current_year=1, last_year_at=NOW() WHERE id=1`)

	log.Println("✅ 全服重置完成，纪年归零")
	return nil
}

// CheckTribulationConditions checks if all 3 conditions are met and marks success
func (e *Engine) CheckTribulationConditions(ctx context.Context, eventID string) (bool, error) {
	var tribYear int
	var element, status string
	var reqCultivatorRealm string
	var reqCultivatorLevel, reqCultivatorCount int
	var reqSpiritStone int64
	var reqMaterialRatio int
	var metCultivators, contributedStone int64

	err := e.db.QueryRow(ctx,
		`SELECT year, element, status,
		        req_cultivator_realm, req_cultivator_level, req_cultivator_count,
		        req_spirit_stone, req_material_ratio,
		        met_cultivators, contributed_spirit_stone
		 FROM tribulation_events WHERE id=$1`,
		eventID,
	).Scan(&tribYear, &element, &status,
		&reqCultivatorRealm, &reqCultivatorLevel, &reqCultivatorCount,
		&reqSpiritStone, &reqMaterialRatio,
		&metCultivators, &contributedStone)
	if err != nil {
		return false, err
	}

	if status != "active" {
		return false, nil
	}

	// Check condition 1: cultivators
	cond1 := metCultivators >= int64(reqCultivatorCount)

	// Check condition 2: spirit stone
	cond2 := contributedStone >= reqSpiritStone

	// Check condition 3: material ratio
	// Get total contributed materials and element-specific
	var totalMat, elemMat int64
	e.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM tribulation_contributions
		 WHERE event_id=$1 AND type='material'`,
		eventID,
	).Scan(&totalMat)
	e.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM tribulation_contributions
		 WHERE event_id=$1 AND type='material' AND element=$2`,
		eventID, element,
	).Scan(&elemMat)

	cond3 := false
	if totalMat > 0 {
		ratio := int(elemMat * 100 / totalMat)
		cond3 = ratio >= reqMaterialRatio
	}

	if cond1 && cond2 && cond3 {
		// Record success and generate hall of fame
		_, _ = e.db.Exec(ctx, `UPDATE tribulation_events SET status='success' WHERE id=$1`, eventID)
		_ = e.recordHallOfFame(ctx, eventID, tribYear, element)
		e.rdb.Publish(ctx, "game:tribulation_success", fmt.Sprintf("year:%d", tribYear))
		log.Printf("🎉 天劫渡过！第%d年 %s系天劫", tribYear, ElementChinese(element))
		return true, nil
	}

	return false, nil
}

// processCaveRewards distributes yearly rewards to cave occupants
func (e *Engine) processCaveRewards(ctx context.Context, currentYear int) error {
	rows, err := e.db.Query(ctx,
		`SELECT cave_id, player_id, last_reward_year FROM cave_occupations WHERE last_reward_year < $1`,
		currentYear,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type caveRewardJob struct {
		CaveID         string
		PlayerID       string
		LastRewardYear int
	}
	var jobs []caveRewardJob
	for rows.Next() {
		var j caveRewardJob
		rows.Scan(&j.CaveID, &j.PlayerID, &j.LastRewardYear)
		jobs = append(jobs, j)
	}
	rows.Close()

	for _, j := range jobs {
		cave, ok := LocationCaves[j.CaveID]
		if !ok {
			continue
		}
		yearsEarned := currentYear - j.LastRewardYear
		_, stonePct, matPct, _ := CaveYearlyReward(cave)

		if stonePct > 0 {
			totalStone := int64(stonePct) * int64(yearsEarned)
			_, _ = e.db.Exec(ctx,
				`UPDATE players SET spirit_stone=spirit_stone+$1, updated_at=NOW() WHERE id=$2`,
				totalStone, j.PlayerID,
			)
		}
		if matPct > 0 {
			bonusMats := int64(matPct/10) * int64(yearsEarned)
			if bonusMats > 0 {
				_, _ = e.db.Exec(ctx,
					`INSERT INTO spirit_materials (player_id, element, quantity) VALUES ($1,$2,$3)
					 ON CONFLICT (player_id, element) DO UPDATE SET quantity=spirit_materials.quantity+$3`,
					j.PlayerID, cave.Element, bonusMats,
				)
			}
		}
		cultivPct, _, _, _ := CaveYearlyReward(cave)
		if cultivPct > 0 {
			bonusXP := int64(BaseXPPerYear) * int64(cultivPct) / 100 * int64(yearsEarned)
			_, _ = e.db.Exec(ctx,
				`UPDATE players SET cultivation_xp=cultivation_xp+$1, updated_at=NOW() WHERE id=$2`,
				bonusXP, j.PlayerID,
			)
		}
		_, _ = e.db.Exec(ctx,
			`UPDATE cave_occupations SET last_reward_year=$1 WHERE cave_id=$2`,
			currentYear, j.CaveID,
		)
	}
	return nil
}

// PublishCaveEvent publishes a cave event to Redis
func (e *Engine) PublishCaveEvent(ctx context.Context, eventType, caveID, playerID string) {
	payload := fmt.Sprintf(`{"caveId":"%s","playerId":"%s"}`, caveID, playerID)
	e.rdb.Publish(ctx, "game:cave:"+eventType, payload)
}

// PublishRealmCompleteEvent publishes a city realm complete event to Redis
func (e *Engine) PublishRealmCompleteEvent(ctx context.Context, cityID, playerID string) {
	payload := fmt.Sprintf(`{"cityId":"%s","playerId":"%s"}`, cityID, playerID)
	e.rdb.Publish(ctx, "game:realm_complete", payload)
}

func (e *Engine) recordHallOfFame(ctx context.Context, eventID string, year int, element string) error {
	// Get battle cultivators
	cultivRows, err := e.db.Query(ctx,
		`SELECT p.username, p.realm, p.realm_level, tc.amount
		 FROM tribulation_contributions tc
		 JOIN players p ON p.id=tc.player_id
		 WHERE tc.event_id=$1 AND tc.type='cultivator'
		 ORDER BY tc.amount DESC`,
		eventID,
	)
	if err != nil {
		return err
	}
	defer cultivRows.Close()

	type cultivatorEntry struct {
		Username  string
		RealmName string
		Amount    int64
	}
	var cultivators []cultivatorEntry
	for cultivRows.Next() {
		var ce cultivatorEntry
		var realm string
		var realmLevel int
		cultivRows.Scan(&ce.Username, &realm, &realmLevel, &ce.Amount)
		ce.RealmName = RealmDisplayName(realm, realmLevel)
		cultivators = append(cultivators, ce)
	}
	cultivRows.Close()

	// Get top material contributors
	matRows, err := e.db.Query(ctx,
		`SELECT p.username, tc.element, SUM(tc.amount) as total
		 FROM tribulation_contributions tc
		 JOIN players p ON p.id=tc.player_id
		 WHERE tc.event_id=$1 AND tc.type='material'
		 GROUP BY p.username, tc.element
		 ORDER BY total DESC LIMIT 3`,
		eventID,
	)
	if err != nil {
		return err
	}
	defer matRows.Close()

	type materialEntry struct {
		Username  string
		Element   string
		ElementCn string
		Total     int64
	}
	var topMaterials []materialEntry
	for matRows.Next() {
		var me materialEntry
		matRows.Scan(&me.Username, &me.Element, &me.Total)
		me.ElementCn = ElementChinese(me.Element)
		topMaterials = append(topMaterials, me)
	}
	matRows.Close()

	// Log for now
	log.Printf("英雄榜 - 第%d年 %s劫: 出战%d人, 材料贡献%d人", year, ElementChinese(element), len(cultivators), len(topMaterials))

	_, err = e.db.Exec(ctx,
		`INSERT INTO hall_of_fame (tribulation_event_id, year, element)
		 VALUES ($1, $2, $3)`,
		eventID, year, element,
	)
	return err
}
