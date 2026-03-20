package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
-- ========== 核心玩家表 ==========
CREATE TABLE IF NOT EXISTS players (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(50) UNIQUE NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,
    agent_id        VARCHAR(50) UNIQUE NOT NULL,
    -- 灵根（随机）
    spirit_root             VARCHAR(20) NOT NULL DEFAULT 'five',
    spirit_root_multiplier  FLOAT NOT NULL DEFAULT 0.8,
    -- 族裔（玩家选择）
    race            VARCHAR(20) NOT NULL DEFAULT 'chinese',
    -- 境界（凡人修仙传）
    realm           VARCHAR(30) NOT NULL DEFAULT 'qi_refining',
    realm_level     INT NOT NULL DEFAULT 1,
    -- 资源
    spirit_stone         BIGINT NOT NULL DEFAULT 100,
    cultivation_xp       BIGINT NOT NULL DEFAULT 0,
    technique_fragment   BIGINT NOT NULL DEFAULT 0,
    soul_sense           BIGINT NOT NULL DEFAULT 100,
    soul_sense_max       BIGINT NOT NULL DEFAULT 100,
    -- 洞府
    cave_level      INT NOT NULL DEFAULT 1,
    -- 功法
    equipped_technique VARCHAR(50) NOT NULL DEFAULT '',
    -- 状态
    is_cultivating  BOOLEAN NOT NULL DEFAULT FALSE,
    joined_epoch    BOOLEAN NOT NULL DEFAULT FALSE,
    last_offline_claim TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 元数据
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ========== 天材地宝表（按五行元素分开存储） ==========
CREATE TABLE IF NOT EXISTS spirit_materials (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id   UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    element     VARCHAR(20) NOT NULL, -- metal/wood/water/fire/earth
    quantity    BIGINT NOT NULL DEFAULT 0,
    UNIQUE(player_id, element)
);

-- ========== 设备登录 ==========
CREATE TABLE IF NOT EXISTS device_login_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        VARCHAR(50) NOT NULL,
    device_name     VARCHAR(100),
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    token           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '10 minutes'
);

-- ========== 物品（突破丹药等） ==========
CREATE TABLE IF NOT EXISTS player_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id   UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    item_id     VARCHAR(50) NOT NULL,
    quantity    INT NOT NULL DEFAULT 1,
    UNIQUE(player_id, item_id)
);

-- ========== 已学习功法 ==========
CREATE TABLE IF NOT EXISTS player_techniques (
    player_id       UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    technique_id    VARCHAR(50) NOT NULL,
    learned_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(player_id, technique_id)
);

-- ========== 秘境探索 ==========
CREATE TABLE IF NOT EXISTS player_explorations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id   UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    realm_id    VARCHAR(50) NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finish_at   TIMESTAMPTZ NOT NULL,
    collected   BOOLEAN NOT NULL DEFAULT FALSE
);

-- ========== 炼丹 ==========
CREATE TABLE IF NOT EXISTS player_alchemy (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id   UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    recipe_id   VARCHAR(50) NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finish_at   TIMESTAMPTZ NOT NULL,
    collected   BOOLEAN NOT NULL DEFAULT FALSE
);

-- ========== 世界状态（全局唯一行） ==========
CREATE TABLE IF NOT EXISTS world_state (
    id              INT PRIMARY KEY DEFAULT 1,
    current_year    INT NOT NULL DEFAULT 1,
    world_started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_year_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ========== 天劫事件 ==========
CREATE TABLE IF NOT EXISTS tribulation_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    year            INT NOT NULL,
    element         VARCHAR(20) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending', -- active/success/failed
    window_start_at TIMESTAMPTZ,
    window_end_at   TIMESTAMPTZ,
    -- 条件参数
    req_cultivator_realm    VARCHAR(30) NOT NULL DEFAULT 'foundation',
    req_cultivator_level    INT NOT NULL DEFAULT 1,
    req_cultivator_count    INT NOT NULL DEFAULT 1,
    req_spirit_stone        BIGINT NOT NULL DEFAULT 500,
    req_material_ratio      INT NOT NULL DEFAULT 30, -- percentage
    -- 当前进度
    met_cultivators         INT NOT NULL DEFAULT 0,
    contributed_spirit_stone BIGINT NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ========== 天劫贡献记录 ==========
CREATE TABLE IF NOT EXISTS tribulation_contributions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id    UUID NOT NULL REFERENCES tribulation_events(id) ON DELETE CASCADE,
    player_id   UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    type        VARCHAR(20) NOT NULL, -- 'cultivator', 'stone', 'material'
    amount      BIGINT NOT NULL DEFAULT 0,
    element     VARCHAR(20), -- for material type
    contributed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ========== 英雄榜 ==========
CREATE TABLE IF NOT EXISTS hall_of_fame (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tribulation_event_id UUID NOT NULL REFERENCES tribulation_events(id),
    year                INT NOT NULL,
    element             VARCHAR(20) NOT NULL,
    recorded_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ========== 洞府占领记录（美国景点） ==========
CREATE TABLE IF NOT EXISTS cave_occupations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cave_id     VARCHAR(50) UNIQUE NOT NULL,  -- e.g. "yellowstone"
    player_id   UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    occupied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_reward_year INT NOT NULL DEFAULT 0
);

-- ========== 城市秘境探索记录 ==========
CREATE TABLE IF NOT EXISTS city_realm_explorations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    city_id         VARCHAR(50) NOT NULL,  -- e.g. "new_york"
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finish_at       TIMESTAMPTZ NOT NULL,
    collected       BOOLEAN NOT NULL DEFAULT FALSE,
    narrative_seed  JSONB  -- event seed generated on enter
);

-- ========== 索引 ==========
CREATE INDEX IF NOT EXISTS idx_device_login_agent ON device_login_requests(agent_id);
CREATE INDEX IF NOT EXISTS idx_device_login_status ON device_login_requests(status);
CREATE INDEX IF NOT EXISTS idx_player_items_player ON player_items(player_id);
CREATE INDEX IF NOT EXISTS idx_player_explorations_player ON player_explorations(player_id);
CREATE INDEX IF NOT EXISTS idx_player_alchemy_player ON player_alchemy(player_id);
CREATE INDEX IF NOT EXISTS idx_spirit_materials_player ON spirit_materials(player_id);
CREATE INDEX IF NOT EXISTS idx_trib_contributions_event ON tribulation_contributions(event_id);
CREATE INDEX IF NOT EXISTS idx_trib_events_status ON tribulation_events(status);
CREATE INDEX IF NOT EXISTS idx_cave_occupations_cave ON cave_occupations(cave_id);
CREATE INDEX IF NOT EXISTS idx_cave_occupations_player ON cave_occupations(player_id);
CREATE INDEX IF NOT EXISTS idx_city_realm_player ON city_realm_explorations(player_id);
`

// migrationSQL handles upgrading existing databases
const migrationSQL = `
-- Add race column if not exists
ALTER TABLE players ADD COLUMN IF NOT EXISTS race VARCHAR(20) NOT NULL DEFAULT 'chinese';

-- Add new resource columns (replacing old ones)
ALTER TABLE players ADD COLUMN IF NOT EXISTS technique_fragment BIGINT NOT NULL DEFAULT 0;
ALTER TABLE players ADD COLUMN IF NOT EXISTS soul_sense BIGINT NOT NULL DEFAULT 100;
ALTER TABLE players ADD COLUMN IF NOT EXISTS soul_sense_max BIGINT NOT NULL DEFAULT 100;
ALTER TABLE players ADD COLUMN IF NOT EXISTS cave_level INT NOT NULL DEFAULT 1;
ALTER TABLE players ADD COLUMN IF NOT EXISTS equipped_technique VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE players ADD COLUMN IF NOT EXISTS last_offline_claim TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Remove old SLG columns
ALTER TABLE players DROP COLUMN IF EXISTS spirit_herb;
ALTER TABLE players DROP COLUMN IF EXISTS mystic_iron;
ALTER TABLE players DROP COLUMN IF EXISTS spirit_wood;
ALTER TABLE players DROP COLUMN IF EXISTS alchemy_material;

-- Remove old tables
DROP TABLE IF EXISTS buildings;
DROP TABLE IF EXISTS game_turns;

-- Initialize world state
INSERT INTO world_state (id, current_year, world_started_at) 
VALUES (1, 1, NOW())
ON CONFLICT (id) DO NOTHING;
`

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	_, err = pool.Exec(ctx, migrationSQL)
	if err != nil {
		return fmt.Errorf("migrate columns: %w", err)
	}
	return nil
}
