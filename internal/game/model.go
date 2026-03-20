package game

import (
	"math/rand"
	"time"
)

// ========== 时间系统 ==========

const (
	// GameYearDuration: 1 game year = 5 real minutes
	GameYearDuration = 5 * time.Minute
)

// ========== 灵根体系 ==========

type SpiritRoot struct {
	Name       string
	Element    string // primary element affinity
	Multiplier float64
	Weight     int
}

var SpiritRoots = []SpiritRoot{
	{Name: "one", Element: "none", Multiplier: 3.0, Weight: 1},   // 天灵根 1%
	{Name: "two", Element: "none", Multiplier: 2.0, Weight: 5},   // 变灵根 5%
	{Name: "three", Element: "none", Multiplier: 1.5, Weight: 15}, // 三灵根 15%
	{Name: "four", Element: "none", Multiplier: 1.0, Weight: 30},  // 四灵根 30%
	{Name: "five", Element: "none", Multiplier: 0.8, Weight: 49},  // 五灵根 49%
}

var SpiritRootNames = map[string]string{
	"one":   "天灵根",
	"two":   "变灵根",
	"three": "三灵根",
	"four":  "四灵根",
	"five":  "五灵根",
}

func RollSpiritRoot() (string, float64) {
	total := 0
	for _, r := range SpiritRoots {
		total += r.Weight
	}
	n := rand.Intn(total)
	acc := 0
	for _, r := range SpiritRoots {
		acc += r.Weight
		if n < acc {
			return r.Name, r.Multiplier
		}
	}
	return "five", 0.8
}

// ========== 族裔体系 ==========

type Race struct {
	ID   string
	Name string
	Desc string
	// Element bonuses (percentage points)
	ElementBonus map[string]int
	// Special ability
	SpecialName string
	SpecialDesc string
	// Numeric gameplay bonuses
	CultivationSpeedPct  int  // +% to all cultivation XP gain
	IdleCultivationPct   int  // +% specifically to idle cultivation
	BreakthroughBonusPct int  // +% to breakthrough success rate
	BreakFailProtect     bool // prevent XP loss on failure (30% chance to trigger)
	RefiningQualityPct   int  // +% to alchemy quality
	FireTechSpeedPct     int  // +% to fire technique speed
}

var Races = map[string]Race{
	"african": {
		ID: "african", Name: "非裔", Desc: "大地之子，根基深厚，美国最古老的传承",
		ElementBonus: map[string]int{"earth": 25, "wood": 15},
		SpecialName:  "不屈根基", SpecialDesc: "突破失败时有30%概率不损失修为",
		BreakFailProtect: true,
	},
	"caucasian": {
		ID: "caucasian", Name: "白裔", Desc: "金水双修，精于炼器，技艺超群",
		ElementBonus: map[string]int{"metal": 25, "water": 10},
		SpecialName:  "炼器天赋", SpecialDesc: "炼器品质+15%",
		RefiningQualityPct: 15,
	},
	"latino": {
		ID: "latino", Name: "拉丁裔", Desc: "烈火传承，功法迅猛，激情四射",
		ElementBonus: map[string]int{"fire": 30, "wood": 10},
		SpecialName:  "烈焰之心", SpecialDesc: "火系功法修炼速度+20%",
		FireTechSpeedPct: 20,
	},
	"chinese": {
		ID: "chinese", Name: "华裔", Desc: "五行均衡，道法自然，根基扎实",
		ElementBonus: map[string]int{"metal": 8, "wood": 8, "water": 8, "fire": 8, "earth": 8},
		SpecialName:  "天道均衡", SpecialDesc: "修炼速度+10%",
		CultivationSpeedPct: 10,
	},
	"indigenous": {
		ID: "indigenous", Name: "原住民", Desc: "自然之灵，万物共鸣，此地真正的主人",
		ElementBonus: map[string]int{"wood": 30, "water": 20},
		SpecialName:  "自然亲和", SpecialDesc: "挂机修为+20%",
		IdleCultivationPct: 20,
	},
	"asian_pacific": {
		ID: "asian_pacific", Name: "亚太裔", Desc: "水金双修，悟性超凡，勤奋刻苦",
		ElementBonus: map[string]int{"water": 25, "metal": 10},
		SpecialName:  "超凡悟性", SpecialDesc: "突破成功率+5%",
		BreakthroughBonusPct: 5,
	},
}

var RaceOrder = []string{"african", "caucasian", "latino", "chinese", "indigenous", "asian_pacific"}

// ========== 境界体系（凡人修仙传） ==========

type RealmTier struct {
	ID         string
	Name       string
	Order      int
	MaxLevel   int
	LevelNames []string
	XPPerLevel []int64
	// Item needed to cross into this realm from previous (empty for qi_refining)
	BreakthroughItem     string
	BreakthroughItemName string
}

var RealmOrder = []string{
	"qi_refining", "foundation", "core_formation", "nascent_soul",
	"deity_transformation", "void_refining", "body_integration",
	"mahayana", "tribulation",
}

var RealmTiers = map[string]RealmTier{
	"qi_refining": {
		ID: "qi_refining", Name: "练气期", Order: 0, MaxLevel: 13,
		LevelNames: []string{
			"一层", "二层", "三层", "四层", "五层", "六层", "七层",
			"八层", "九层", "十层", "十一层", "十二层", "十三层",
		},
		// 10万每层
		XPPerLevel: []int64{
			100000, 100000, 100000, 100000, 100000, 100000, 100000,
			100000, 100000, 100000, 100000, 100000, 100000,
		},
	},
	"foundation": {
		ID: "foundation", Name: "筑基期", Order: 1, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{1500000, 2500000, 4000000, 6000000},
		BreakthroughItem:     "foundation_pill",
		BreakthroughItemName: "筑基丹",
	},
	"core_formation": {
		ID: "core_formation", Name: "结丹期", Order: 2, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{7000000, 12000000, 18000000, 25000000},
		BreakthroughItem:     "core_pill",
		BreakthroughItemName: "结丹丹",
	},
	"nascent_soul": {
		ID: "nascent_soul", Name: "元婴期", Order: 3, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{30000000, 50000000, 75000000, 100000000},
		BreakthroughItem:     "nascent_pill",
		BreakthroughItemName: "凝婴丹",
	},
	"deity_transformation": {
		ID: "deity_transformation", Name: "化神期", Order: 4, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{200000000, 350000000, 500000000, 750000000},
		BreakthroughItem:     "deity_pill",
		BreakthroughItemName: "化神丹",
	},
	"void_refining": {
		ID: "void_refining", Name: "炼虚期", Order: 5, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{800000000, 1200000000, 1800000000, 2500000000},
		BreakthroughItem:     "void_pill",
		BreakthroughItemName: "炼虚丹",
	},
	"body_integration": {
		ID: "body_integration", Name: "合体期", Order: 6, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{3000000000, 4500000000, 6500000000, 9000000000},
		BreakthroughItem:     "integration_pill",
		BreakthroughItemName: "合体丹",
	},
	"mahayana": {
		ID: "mahayana", Name: "大乘期", Order: 7, MaxLevel: 4,
		LevelNames:           []string{"初期", "中期", "后期", "大圆满"},
		XPPerLevel:           []int64{10000000000, 15000000000, 22000000000, 32000000000},
		BreakthroughItem:     "mahayana_pill",
		BreakthroughItemName: "大乘丹",
	},
	"tribulation": {
		ID: "tribulation", Name: "渡劫期", Order: 8, MaxLevel: 3,
		LevelNames:           []string{"三九天劫", "六九天劫", "九九天劫"},
		XPPerLevel:           []int64{50000000000, 100000000000, 200000000000},
		BreakthroughItem:     "tribulation_pill",
		BreakthroughItemName: "渡劫丹",
	},
}

// GetXPNeeded returns XP needed for the current realm+level stage
func GetXPNeeded(realm string, level int) int64 {
	tier, ok := RealmTiers[realm]
	if !ok {
		return 999999999999
	}
	if level < 1 || level > tier.MaxLevel {
		return 999999999999
	}
	return tier.XPPerLevel[level-1]
}

// NextRealmLevel returns what realm/level comes after breakthrough.
// isMajor indicates a cross-realm breakthrough (needs item).
func NextRealmLevel(realm string, level int) (newRealm string, newLevel int, isMajor bool) {
	tier, ok := RealmTiers[realm]
	if !ok {
		return "", 0, false
	}
	if level < tier.MaxLevel {
		return realm, level + 1, false
	}
	// Need to advance to next realm
	idx := -1
	for i, r := range RealmOrder {
		if r == realm {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(RealmOrder)-1 {
		return "", 0, false // at max realm
	}
	return RealmOrder[idx+1], 1, true
}

// RealmAtLeast checks if player's realm/level >= required realm/level
func RealmAtLeast(playerRealm string, playerLevel int, minRealm string, minLevel int) bool {
	pt, ok1 := RealmTiers[playerRealm]
	mt, ok2 := RealmTiers[minRealm]
	if !ok1 || !ok2 {
		return false
	}
	if pt.Order > mt.Order {
		return true
	}
	if pt.Order == mt.Order {
		return playerLevel >= minLevel
	}
	return false
}

// RealmDisplayName returns human-readable realm+level name
func RealmDisplayName(realm string, level int) string {
	tier, ok := RealmTiers[realm]
	if !ok {
		return realm
	}
	if level >= 1 && level <= len(tier.LevelNames) {
		return tier.Name + tier.LevelNames[level-1]
	}
	return tier.Name
}

// ========== 天劫系统 ==========

// TribulationSchedule defines when tribulations occur and their requirements
type TribulationSchedule struct {
	Year    int
	Element string // "earth", "fire", "metal", "water", "wood"
	// Requirement 1: cultivators
	ReqCultivatorRealm string
	ReqCultivatorLevel int
	ReqCultivatorCount int
	// Requirement 2: spirit stones
	ReqSpiritStone int64
	// Requirement 3: element materials (ratio as percentage)
	ReqMaterialRatio int // percentage of total materials that must be this element
	// Window duration
	WindowHours int
}

var TribulationSchedules = []TribulationSchedule{
	{
		Year: 100, Element: "earth",
		ReqCultivatorRealm: "foundation", ReqCultivatorLevel: 1, ReqCultivatorCount: 1,
		ReqSpiritStone: 500,
		ReqMaterialRatio: 30,
		WindowHours: 1,
	},
	{
		Year: 300, Element: "fire",
		ReqCultivatorRealm: "core_formation", ReqCultivatorLevel: 1, ReqCultivatorCount: 3,
		ReqSpiritStone: 10000,
		ReqMaterialRatio: 50,
		WindowHours: 1,
	},
	{
		Year: 600, Element: "metal",
		ReqCultivatorRealm: "nascent_soul", ReqCultivatorLevel: 1, ReqCultivatorCount: 3,
		ReqSpiritStone: 50000,
		ReqMaterialRatio: 60,
		WindowHours: 1,
	},
}

// GetTribulationSchedule returns the schedule for a given year, nil if none
func GetTribulationSchedule(year int) *TribulationSchedule {
	for i := range TribulationSchedules {
		if TribulationSchedules[i].Year == year {
			return &TribulationSchedules[i]
		}
	}
	// After first 3, tribulations continue every 300 years with scaling requirements
	if year > 600 && year <= 3000 && year%300 == 0 {
		epoch := (year - 600) / 300
		elements := []string{"water", "wood", "earth", "fire", "metal"}
		elem := elements[epoch%len(elements)]
		multiplier := int64(1 << uint(epoch)) // 2^epoch scaling
		return &TribulationSchedule{
			Year:               year,
			Element:            elem,
			ReqCultivatorRealm: "deity_transformation",
			ReqCultivatorLevel: 1,
			ReqCultivatorCount: epoch + 3,
			ReqSpiritStone:     200000 * multiplier,
			ReqMaterialRatio:   60,
			WindowHours:        1,
		}
	}
	return nil
}

// NextTribulationYear returns the next tribulation year >= currentYear
func NextTribulationYear(currentYear int) (year int, element string) {
	// Check fixed schedules
	for _, s := range TribulationSchedules {
		if s.Year >= currentYear {
			return s.Year, s.Element
		}
	}
	// Generate dynamic schedule
	for y := 900; y <= 3000; y += 300 {
		if y >= currentYear {
			s := GetTribulationSchedule(y)
			if s != nil {
				return y, s.Element
			}
		}
	}
	return 3000, "all"
}

// ElementChinese converts element ID to Chinese name
func ElementChinese(element string) string {
	switch element {
	case "metal":
		return "金"
	case "wood":
		return "木"
	case "water":
		return "水"
	case "fire":
		return "火"
	case "earth":
		return "土"
	case "all":
		return "五行"
	}
	return element
}

// ========== 功法体系 ==========

type Technique struct {
	ID         string
	Name       string
	Element    string // "fire", "water", "metal", "wood", "earth", "all", "none"
	MinRealm   string
	MinLevel   int
	FragCost   int
	XPBonusPct float64 // +% to cultivation XP
	Desc       string
}

var TechniqueOrder = []string{"basic_breathing", "five_elements", "purple_cloud", "azure_essence", "great_expansion"}

var Techniques = map[string]Technique{
	"basic_breathing": {
		ID: "basic_breathing", Name: "基础吐纳术", Element: "none",
		MinRealm: "qi_refining", MinLevel: 1, FragCost: 0,
		XPBonusPct: 0, Desc: "最基础的修炼法门，人人可学",
	},
	"five_elements": {
		ID: "five_elements", Name: "五行聚灵诀", Element: "all",
		MinRealm: "qi_refining", MinLevel: 5, FragCost: 10,
		XPBonusPct: 10, Desc: "聚五行灵气，修炼速度+10%",
	},
	"purple_cloud": {
		ID: "purple_cloud", Name: "紫霞神功", Element: "fire",
		MinRealm: "foundation", MinLevel: 1, FragCost: 30,
		XPBonusPct: 20, Desc: "紫霞真火淬体，修炼速度+20%",
	},
	"azure_essence": {
		ID: "azure_essence", Name: "太清化元功", Element: "water",
		MinRealm: "core_formation", MinLevel: 1, FragCost: 80,
		XPBonusPct: 35, Desc: "太清仙法化元归真，修炼速度+35%",
	},
	"great_expansion": {
		ID: "great_expansion", Name: "大衍真经", Element: "none",
		MinRealm: "nascent_soul", MinLevel: 1, FragCost: 200,
		XPBonusPct: 50, Desc: "上古真经大衍无极，修炼速度+50%",
	},
}

// ========== 秘境体系（含天材地宝属性） ==========

type SecretRealmRewards struct {
	SpiritStone      [2]int64 // [min, max]
	MaterialPerElem  [2]int64 // spirit_material per element
	TechFragment     [2]int64
}

type SecretRealm struct {
	ID          string
	Name        string
	MinRealm    string
	SoulCost    int64
	DurationSec int
	Rewards     SecretRealmRewards
	Desc        string
}

var SecretRealmOrder = []string{"herb_valley", "treasure_mountain", "alchemy_ruins", "ancient_cave"}

var SecretRealms = map[string]SecretRealm{
	"herb_valley": {
		ID: "herb_valley", Name: "灵药谷", MinRealm: "qi_refining",
		SoulCost: 20, DurationSec: 300,
		Rewards: SecretRealmRewards{
			SpiritStone:     [2]int64{50, 200},
			MaterialPerElem: [2]int64{2, 8},
			TechFragment:    [2]int64{0, 3},
		},
		Desc: "充满灵药的山谷，天材地宝丰盛",
	},
	"treasure_mountain": {
		ID: "treasure_mountain", Name: "宝药山", MinRealm: "foundation",
		SoulCost: 40, DurationSec: 600,
		Rewards: SecretRealmRewards{
			SpiritStone:     [2]int64{200, 800},
			MaterialPerElem: [2]int64{5, 20},
			TechFragment:    [2]int64{2, 8},
		},
		Desc: "蕴含丰富宝药的仙山，需筑基期以上修为",
	},
	"alchemy_ruins": {
		ID: "alchemy_ruins", Name: "炼丹炉遗迹", MinRealm: "core_formation",
		SoulCost: 60, DurationSec: 900,
		Rewards: SecretRealmRewards{
			SpiritStone:     [2]int64{500, 2000},
			MaterialPerElem: [2]int64{10, 40},
			TechFragment:    [2]int64{5, 15},
		},
		Desc: "远古炼丹师的遗迹，可能找到珍贵丹方",
	},
	"ancient_cave": {
		ID: "ancient_cave", Name: "远古修士洞府", MinRealm: "nascent_soul",
		SoulCost: 80, DurationSec: 1800,
		Rewards: SecretRealmRewards{
			SpiritStone:     [2]int64{1000, 5000},
			MaterialPerElem: [2]int64{20, 80},
			TechFragment:    [2]int64{10, 30},
		},
		Desc: "远古大能的洞府，危险与机缘并存",
	},
}

var Elements = []string{"metal", "wood", "water", "fire", "earth"}

// RollRewards generates random rewards from a secret realm, including element-attributed materials
func RollRewards(sr SecretRealm) map[string]int64 {
	rewards := make(map[string]int64)
	if sr.Rewards.SpiritStone[1] > 0 {
		r := sr.Rewards.SpiritStone
		rewards["spirit_stone"] = r[0] + rand.Int63n(r[1]-r[0]+1)
	}
	if sr.Rewards.TechFragment[1] > 0 {
		r := sr.Rewards.TechFragment
		rewards["technique_fragment"] = r[0] + rand.Int63n(r[1]-r[0]+1)
	}
	// Roll materials for each element
	if sr.Rewards.MaterialPerElem[1] > 0 {
		r := sr.Rewards.MaterialPerElem
		for _, elem := range Elements {
			qty := r[0] + rand.Int63n(r[1]-r[0]+1)
			if qty > 0 {
				rewards["material_"+elem] = qty
			}
		}
	}
	return rewards
}

// ========== 炼丹体系 ==========

// AlchemyMaterialCost represents cost in terms of element-specific materials
type AlchemyMaterialCost struct {
	Element  string
	Quantity int64
}

type AlchemyRecipe struct {
	ID           string
	Name         string
	MaterialCosts []AlchemyMaterialCost // element-specific material costs
	DurationSec  int
	MinRealm     string
	OutputItem   string // item ID produced (empty = direct XP)
	OutputQty    int
	DirectXP     int64 // if OutputItem is empty, give XP directly
	Desc         string
}

var AlchemyRecipeOrder = []string{"xp_pill", "foundation_pill", "core_pill", "nascent_pill", "deity_pill"}

var AlchemyRecipes = map[string]AlchemyRecipe{
	"xp_pill": {
		ID: "xp_pill", Name: "聚灵丹",
		MaterialCosts: []AlchemyMaterialCost{{"earth", 5}, {"wood", 5}},
		DurationSec: 180,
		MinRealm: "qi_refining",
		DirectXP: 50000,
		Desc:     "提升修为的基础丹药，服用后直接获得50000修为",
	},
	"foundation_pill": {
		ID: "foundation_pill", Name: "筑基丹",
		MaterialCosts: []AlchemyMaterialCost{{"earth", 30}, {"wood", 20}},
		DurationSec: 600,
		MinRealm: "qi_refining",
		OutputItem: "foundation_pill", OutputQty: 1,
		Desc: "练气突破筑基的必需丹药，需要土系和木系天材地宝",
	},
	"core_pill": {
		ID: "core_pill", Name: "结丹丹",
		MaterialCosts: []AlchemyMaterialCost{{"fire", 50}, {"wood", 30}, {"earth", 20}},
		DurationSec: 1200,
		MinRealm: "foundation",
		OutputItem: "core_pill", OutputQty: 1,
		Desc: "筑基突破结丹的必需丹药，需要火系天材地宝为主",
	},
	"nascent_pill": {
		ID: "nascent_pill", Name: "凝婴丹",
		MaterialCosts: []AlchemyMaterialCost{{"metal", 80}, {"water", 60}, {"fire", 40}},
		DurationSec: 1800,
		MinRealm: "core_formation",
		OutputItem: "nascent_pill", OutputQty: 1,
		Desc: "结丹突破元婴的必需丹药，需要金系为主的天材地宝",
	},
	"deity_pill": {
		ID: "deity_pill", Name: "化神丹",
		MaterialCosts: []AlchemyMaterialCost{{"metal", 200}, {"water", 150}, {"wood", 100}, {"fire", 100}, {"earth", 50}},
		DurationSec: 3600,
		MinRealm: "nascent_soul",
		OutputItem: "deity_pill", OutputQty: 1,
		Desc: "元婴突破化神的必需丹药，需要五行天材地宝",
	},
}

// ========== 洞府系统（简化） ==========

// CaveIdleBonus returns the idle cultivation bonus multiplier from cave level
// Level 1 = 0% bonus, each additional level = +5%
func CaveIdleBonus(level int) float64 {
	if level <= 1 {
		return 0
	}
	return float64(level-1) * 0.05
}

// ========== 洞府系统（美国景点，可占领） ==========

// CaveBonusType describes what stat is boosted
type CaveBonusType string

const (
	CaveBonusCultivation    CaveBonusType = "cultivation"    // +% 修炼速度
	CaveBonusSpiritStone    CaveBonusType = "spirit_stone"   // 每年额外灵石
	CaveBonusMaterial       CaveBonusType = "material"       // 天材地宝+%
	CaveBonusBreakthrough   CaveBonusType = "breakthrough"   // 突破成功率+%
)

type LocationCave struct {
	ID          string
	Name        string
	NameEn      string
	Element     string // metal/wood/water/fire/earth
	BonusType   CaveBonusType
	BonusValue  int // percentage or flat value
	Desc        string
}

var CaveOrder = []string{
	"yellowstone", "grand_canyon", "seventeen_mile", "yosemite", "arches",
	"death_valley", "great_smoky", "glacier", "hawaii_volcanoes", "niagara",
	"zion", "bryce_canyon", "olympic", "acadia", "sequoia",
	"joshua_tree", "carlsbad", "white_sands", "painted_desert", "craters_moon",
	"great_sand_dunes", "saguaro", "canyonlands", "mt_rainier", "mt_st_helens",
	"everglades", "mammoth_cave", "cape_cod", "big_sur", "badlands",
}

var LocationCaves = map[string]LocationCave{
	"yellowstone":       {ID: "yellowstone", Name: "黄石仙域", NameEn: "Yellowstone", Element: "fire", BonusType: CaveBonusCultivation, BonusValue: 15, Desc: "火山地热孕育的仙域，修炼速度大幅提升"},
	"grand_canyon":      {ID: "grand_canyon", Name: "大峡谷秘府", NameEn: "Grand Canyon", Element: "earth", BonusType: CaveBonusSpiritStone, BonusValue: 20, Desc: "亿万年大地之力凝聚，灵石源源不断"},
	"seventeen_mile":    {ID: "seventeen_mile", Name: "17里云道", NameEn: "17-Mile Drive", Element: "metal", BonusType: CaveBonusCultivation, BonusValue: 10, Desc: "金系灵气弥漫的沿海仙道"},
	"yosemite":          {ID: "yosemite", Name: "优胜美地洞天", NameEn: "Yosemite", Element: "wood", BonusType: CaveBonusCultivation, BonusValue: 12, Desc: "参天古木庇护，木灵之气充沛"},
	"arches":            {ID: "arches", Name: "拱门灵穴", NameEn: "Arches NP", Element: "earth", BonusType: CaveBonusSpiritStone, BonusValue: 15, Desc: "天然拱门蕴含土系灵气，聚石之地"},
	"death_valley":      {ID: "death_valley", Name: "死亡谷炼炉", NameEn: "Death Valley", Element: "fire", BonusType: CaveBonusMaterial, BonusValue: 25, Desc: "极端炎热锻造天材，天材地宝产出极丰"},
	"great_smoky":       {ID: "great_smoky", Name: "大烟山隐府", NameEn: "Great Smoky Mountains", Element: "wood", BonusType: CaveBonusCultivation, BonusValue: 10, Desc: "云雾缭绕古山，木灵之气滋养"},
	"glacier":           {ID: "glacier", Name: "冰川仙境", NameEn: "Glacier NP", Element: "water", BonusType: CaveBonusCultivation, BonusValue: 12, Desc: "千年冰川蕴含水系仙机"},
	"hawaii_volcanoes":  {ID: "hawaii_volcanoes", Name: "夏威夷火山道场", NameEn: "Hawaii Volcanoes", Element: "fire", BonusType: CaveBonusBreakthrough, BonusValue: 5, Desc: "活火山之力助力境界突破"},
	"niagara":           {ID: "niagara", Name: "尼亚加拉瀑布洞", NameEn: "Niagara Falls", Element: "water", BonusType: CaveBonusSpiritStone, BonusValue: 18, Desc: "磅礴瀑布之下，水灵聚石"},
	"zion":              {ID: "zion", Name: "锡安峡谷", NameEn: "Zion NP", Element: "earth", BonusType: CaveBonusCultivation, BonusValue: 10, Desc: "红岩峡谷土系灵气浓郁"},
	"bryce_canyon":      {ID: "bryce_canyon", Name: "布莱斯峡谷", NameEn: "Bryce Canyon", Element: "earth", BonusType: CaveBonusMaterial, BonusValue: 20, Desc: "奇特地貌孕育稀有天材地宝"},
	"olympic":           {ID: "olympic", Name: "奥林匹克云峰", NameEn: "Olympic NP", Element: "wood", BonusType: CaveBonusCultivation, BonusValue: 8, Desc: "雨林生态木灵丰沛"},
	"acadia":            {ID: "acadia", Name: "阿卡迪亚海崖", NameEn: "Acadia NP", Element: "water", BonusType: CaveBonusSpiritStone, BonusValue: 15, Desc: "海崖之上水灵汇聚成石"},
	"sequoia":           {ID: "sequoia", Name: "红杉仙木林", NameEn: "Sequoia NP", Element: "wood", BonusType: CaveBonusCultivation, BonusValue: 15, Desc: "万年红杉木灵之气直冲云霄"},
	"joshua_tree":       {ID: "joshua_tree", Name: "约书亚树荒原", NameEn: "Joshua Tree", Element: "metal", BonusType: CaveBonusMaterial, BonusValue: 15, Desc: "金系荒漠蕴藏丰富矿脉天材"},
	"carlsbad":          {ID: "carlsbad", Name: "卡尔斯巴德地窟", NameEn: "Carlsbad Caverns", Element: "earth", BonusType: CaveBonusCultivation, BonusValue: 8, Desc: "地下洞窟土系灵气沉积"},
	"white_sands":       {ID: "white_sands", Name: "白沙幻境", NameEn: "White Sands", Element: "metal", BonusType: CaveBonusCultivation, BonusValue: 10, Desc: "纯白沙漠金系灵气奇特"},
	"painted_desert":    {ID: "painted_desert", Name: "彩绘沙漠", NameEn: "Painted Desert", Element: "fire", BonusType: CaveBonusMaterial, BonusValue: 18, Desc: "五彩岩层火系天材丰盛"},
	"craters_moon":      {ID: "craters_moon", Name: "月亮火山坑", NameEn: "Craters of the Moon", Element: "fire", BonusType: CaveBonusCultivation, BonusValue: 12, Desc: "熔岩地貌火系灵气异常活跃"},
	"great_sand_dunes":  {ID: "great_sand_dunes", Name: "大沙丘仙台", NameEn: "Great Sand Dunes", Element: "earth", BonusType: CaveBonusCultivation, BonusValue: 8, Desc: "大陆高处土系灵气聚集"},
	"saguaro":           {ID: "saguaro", Name: "仙人掌森林", NameEn: "Saguaro NP", Element: "wood", BonusType: CaveBonusMaterial, BonusValue: 15, Desc: "沙漠木系奇植孕育天材"},
	"canyonlands":       {ID: "canyonlands", Name: "峡谷地迷宫", NameEn: "Canyonlands", Element: "earth", BonusType: CaveBonusSpiritStone, BonusValue: 12, Desc: "峡谷迷宫藏匿灵石脉络"},
	"mt_rainier":        {ID: "mt_rainier", Name: "雷尼尔雪峰", NameEn: "Mt. Rainier", Element: "water", BonusType: CaveBonusCultivation, BonusValue: 10, Desc: "冰雪覆盖高峰水系灵气浓厚"},
	"mt_st_helens":      {ID: "mt_st_helens", Name: "圣海伦火山", NameEn: "Mt. St. Helens", Element: "fire", BonusType: CaveBonusMaterial, BonusValue: 20, Desc: "活火山爆发遗迹火系天材极丰"},
	"everglades":        {ID: "everglades", Name: "大沼泽秘地", NameEn: "Everglades", Element: "water", BonusType: CaveBonusCultivation, BonusValue: 8, Desc: "热带湿地水系灵气滋润"},
	"mammoth_cave":      {ID: "mammoth_cave", Name: "猛犸洞天", NameEn: "Mammoth Cave", Element: "earth", BonusType: CaveBonusSpiritStone, BonusValue: 10, Desc: "世界最长洞穴系统土系灵石遍布"},
	"cape_cod":          {ID: "cape_cod", Name: "科德角海府", NameEn: "Cape Cod", Element: "water", BonusType: CaveBonusSpiritStone, BonusValue: 15, Desc: "海湾水系灵力汇聚成石"},
	"big_sur":           {ID: "big_sur", Name: "大苏尔仙崖", NameEn: "Big Sur", Element: "metal", BonusType: CaveBonusCultivation, BonusValue: 12, Desc: "绝壁金系灵气磅礴"},
	"badlands":          {ID: "badlands", Name: "蒙大拿草原", NameEn: "Badlands", Element: "metal", BonusType: CaveBonusMaterial, BonusValue: 12, Desc: "荒凉大地金系矿脉隐藏其中"},
}

// CaveYearlyReward returns the yearly reward for a cave occupant
// Returns: cultivationPct (bonus %), spiritStoneFlat (per year), materialPct (bonus %), breakthroughBonus (%)
func CaveYearlyReward(cave LocationCave) (cultivationPct, spiritStoneFlat, materialPct, breakthroughBonus int) {
	switch cave.BonusType {
	case CaveBonusCultivation:
		cultivationPct = cave.BonusValue
	case CaveBonusSpiritStone:
		spiritStoneFlat = cave.BonusValue * 10 // scale: 20% -> 200 stones/year
	case CaveBonusMaterial:
		materialPct = cave.BonusValue
	case CaveBonusBreakthrough:
		breakthroughBonus = cave.BonusValue
	}
	return
}

// ========== 城市秘境系统（30个美国城市，挂机探索） ==========

type CityRealm struct {
	ID             string
	Name           string
	NameEn         string
	Elements       []string // can have multiple
	DurationSec    int      // real seconds (hours * 3600)
	SoulCost       int64
	// Base rewards (scaled by city size)
	BaseXP         int64
	BaseSpiritStone int64
	BaseMaterials  int // count per element
	Desc           string
}

// encounter types for narrative seeds
var EncounterTypes = []string{"ancient_ruin", "boss_monster", "treasure_cache", "mysterious_merchant", "faction_conflict"}

var CityRealmOrder = []string{
	"new_york", "los_angeles", "chicago", "houston", "phoenix",
	"philadelphia", "san_antonio", "san_diego", "dallas", "san_francisco",
	"seattle", "boston", "denver", "miami", "atlanta",
	"detroit", "minneapolis", "st_louis", "new_orleans", "portland",
	"las_vegas", "salt_lake_city", "albuquerque", "austin", "nashville",
	"charlotte", "columbus", "indianapolis", "jacksonville", "baltimore",
}

var CityRealms = map[string]CityRealm{
	"new_york":       {ID: "new_york", Name: "纽约·曼哈顿", NameEn: "New York", Elements: []string{"metal"}, DurationSec: 8 * 3600, SoulCost: 12, BaseXP: 4500, BaseSpiritStone: 200, BaseMaterials: 2, Desc: "金融之都，金系灵气极为浓郁"},
	"los_angeles":    {ID: "los_angeles", Name: "洛杉矶·好莱坞", NameEn: "Los Angeles", Elements: []string{"fire"}, DurationSec: 6 * 3600, SoulCost: 10, BaseXP: 3500, BaseSpiritStone: 160, BaseMaterials: 2, Desc: "娱乐之都，火系灵气热烈奔放"},
	"chicago":        {ID: "chicago", Name: "芝加哥·风城", NameEn: "Chicago", Elements: []string{"metal", "water"}, DurationSec: 5 * 3600, SoulCost: 9, BaseXP: 3000, BaseSpiritStone: 140, BaseMaterials: 2, Desc: "风城双修，金水交融"},
	"houston":        {ID: "houston", Name: "休斯顿·炼油城", NameEn: "Houston", Elements: []string{"fire", "earth"}, DurationSec: 4 * 3600, SoulCost: 8, BaseXP: 2500, BaseSpiritStone: 120, BaseMaterials: 2, Desc: "石油之城，火土双系灵气旺盛"},
	"phoenix":        {ID: "phoenix", Name: "凤凰城·烈焰", NameEn: "Phoenix", Elements: []string{"fire"}, DurationSec: 3 * 3600, SoulCost: 7, BaseXP: 2000, BaseSpiritStone: 100, BaseMaterials: 1, Desc: "沙漠火城，火系灵气炽热"},
	"philadelphia":   {ID: "philadelphia", Name: "费城·古都", NameEn: "Philadelphia", Elements: []string{"earth"}, DurationSec: 3 * 3600, SoulCost: 7, BaseXP: 2000, BaseSpiritStone: 100, BaseMaterials: 1, Desc: "历史古都，土系灵气深厚"},
	"san_antonio":    {ID: "san_antonio", Name: "圣安东尼奥·边城", NameEn: "San Antonio", Elements: []string{"earth"}, DurationSec: 3 * 3600, SoulCost: 6, BaseXP: 1800, BaseSpiritStone: 90, BaseMaterials: 1, Desc: "边境之城，土系灵气绵延"},
	"san_diego":      {ID: "san_diego", Name: "圣迭戈·海湾", NameEn: "San Diego", Elements: []string{"water"}, DurationSec: 3 * 3600, SoulCost: 6, BaseXP: 1800, BaseSpiritStone: 90, BaseMaterials: 1, Desc: "海湾城市，水系灵气充盈"},
	"dallas":         {ID: "dallas", Name: "达拉斯·牛仔城", NameEn: "Dallas", Elements: []string{"metal"}, DurationSec: 3 * 3600, SoulCost: 7, BaseXP: 2000, BaseSpiritStone: 100, BaseMaterials: 1, Desc: "金融牛仔城，金系灵气聚集"},
	"san_francisco":  {ID: "san_francisco", Name: "旧金山·金门", NameEn: "San Francisco", Elements: []string{"water", "metal"}, DurationSec: 2 * 3600, SoulCost: 6, BaseXP: 1500, BaseSpiritStone: 80, BaseMaterials: 1, Desc: "金门之城，水金交汇"},
	"seattle":        {ID: "seattle", Name: "西雅图·雨城", NameEn: "Seattle", Elements: []string{"water", "wood"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "常年烟雨，水木共生"},
	"boston":         {ID: "boston", Name: "波士顿·学城", NameEn: "Boston", Elements: []string{"water"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "学术重镇，水系灵气清澈"},
	"denver":         {ID: "denver", Name: "丹佛·高原", NameEn: "Denver", Elements: []string{"metal", "wood"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "高原之城，金木双修"},
	"miami":          {ID: "miami", Name: "迈阿密·热浪", NameEn: "Miami", Elements: []string{"water", "fire"}, DurationSec: int(1.5 * 3600), SoulCost: 4, BaseXP: 900, BaseSpiritStone: 55, BaseMaterials: 1, Desc: "热带海滨，水火激荡"},
	"atlanta":        {ID: "atlanta", Name: "亚特兰大·枢纽", NameEn: "Atlanta", Elements: []string{"wood", "fire"}, DurationSec: int(1.5 * 3600), SoulCost: 4, BaseXP: 900, BaseSpiritStone: 55, BaseMaterials: 1, Desc: "南方枢纽，木火旺盛"},
	"detroit":        {ID: "detroit", Name: "底特律·钢铁城", NameEn: "Detroit", Elements: []string{"metal"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "汽车钢铁之城，金系灵气厚重"},
	"minneapolis":    {ID: "minneapolis", Name: "明尼阿波利斯·湖城", NameEn: "Minneapolis", Elements: []string{"water"}, DurationSec: int(1.5 * 3600), SoulCost: 4, BaseXP: 900, BaseSpiritStone: 55, BaseMaterials: 1, Desc: "千湖之城，水系灵气充裕"},
	"st_louis":       {ID: "st_louis", Name: "圣路易斯·门城", NameEn: "St. Louis", Elements: []string{"earth"}, DurationSec: 1 * 3600, SoulCost: 3, BaseXP: 600, BaseSpiritStone: 40, BaseMaterials: 1, Desc: "西大门土系灵气稳固"},
	"new_orleans":    {ID: "new_orleans", Name: "新奥尔良·爵士城", NameEn: "New Orleans", Elements: []string{"water", "wood"}, DurationSec: int(1.5 * 3600), SoulCost: 4, BaseXP: 900, BaseSpiritStone: 55, BaseMaterials: 1, Desc: "爵士之都，水木交融神秘"},
	"portland":       {ID: "portland", Name: "波特兰·玫瑰城", NameEn: "Portland", Elements: []string{"wood", "water"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "玫瑰花城，木水滋养"},
	"las_vegas":      {ID: "las_vegas", Name: "拉斯维加斯·幻城", NameEn: "Las Vegas", Elements: []string{"fire", "metal"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "不夜幻城，火金交织"},
	"salt_lake_city": {ID: "salt_lake_city", Name: "盐湖城·圣地", NameEn: "Salt Lake City", Elements: []string{"earth"}, DurationSec: 1 * 3600, SoulCost: 3, BaseXP: 600, BaseSpiritStone: 40, BaseMaterials: 1, Desc: "盐湖圣地，土系灵气净化"},
	"albuquerque":    {ID: "albuquerque", Name: "阿尔伯克基·热气球", NameEn: "Albuquerque", Elements: []string{"earth", "fire"}, DurationSec: int(1.5 * 3600), SoulCost: 4, BaseXP: 900, BaseSpiritStone: 55, BaseMaterials: 1, Desc: "沙漠热气球，土火弥漫"},
	"austin":         {ID: "austin", Name: "奥斯汀·创新城", NameEn: "Austin", Elements: []string{"wood", "fire"}, DurationSec: 2 * 3600, SoulCost: 5, BaseXP: 1200, BaseSpiritStone: 70, BaseMaterials: 1, Desc: "创新之城，木火并进"},
	"nashville":      {ID: "nashville", Name: "纳什维尔·音乐城", NameEn: "Nashville", Elements: []string{"wood"}, DurationSec: 2 * 3600, SoulCost: 4, BaseXP: 1000, BaseSpiritStone: 60, BaseMaterials: 1, Desc: "音乐之都，木系灵气悠扬"},
	"charlotte":      {ID: "charlotte", Name: "夏洛特·金融城", NameEn: "Charlotte", Elements: []string{"wood", "earth"}, DurationSec: 2 * 3600, SoulCost: 4, BaseXP: 1000, BaseSpiritStone: 60, BaseMaterials: 1, Desc: "南方金融，木土平衡"},
	"columbus":       {ID: "columbus", Name: "哥伦布·心城", NameEn: "Columbus", Elements: []string{"earth"}, DurationSec: 2 * 3600, SoulCost: 4, BaseXP: 1000, BaseSpiritStone: 60, BaseMaterials: 1, Desc: "大陆心脏，土系灵气平稳"},
	"indianapolis":   {ID: "indianapolis", Name: "印第安纳波利斯·赛城", NameEn: "Indianapolis", Elements: []string{"earth", "metal"}, DurationSec: 2 * 3600, SoulCost: 4, BaseXP: 1000, BaseSpiritStone: 60, BaseMaterials: 1, Desc: "赛道之城，土金交叠"},
	"jacksonville":   {ID: "jacksonville", Name: "杰克逊维尔·河城", NameEn: "Jacksonville", Elements: []string{"water", "wood"}, DurationSec: 2 * 3600, SoulCost: 4, BaseXP: 1000, BaseSpiritStone: 60, BaseMaterials: 1, Desc: "河流之城，水木共融"},
	"baltimore":      {ID: "baltimore", Name: "巴尔的摩·港城", NameEn: "Baltimore", Elements: []string{"water"}, DurationSec: int(1.5 * 3600), SoulCost: 4, BaseXP: 900, BaseSpiritStone: 55, BaseMaterials: 1, Desc: "海港之城，水系灵气汇聚"},
}

// CityNarrativeHints provides flavor hints for narrative seed generation
var CityNarrativeHints = map[string][]string{
	"new_york":       {"在曼哈顿地下发现了一处上古修士遗迹，华尔街金融灵晶散落一地", "帝国大厦顶层封印着一头金系妖兽，守护着无尽宝藏", "中央公园深处有一处隐秘秘境入口"},
	"los_angeles":    {"好莱坞星光大道封印着火系阵法", "比佛利山庄豪宅地下暗藏炼火宝地", "圣莫尼卡海滩水火交汇之处发现奇异灵脉"},
	"chicago":        {"芝加哥风城中风系阵法与金系矿脉交叠", "密歇根湖底沉睡着水系古修士洞府", "摩天大楼群中金系灵气形成天然阵法"},
	"houston":        {"石油管道下方火系灵脉异常活跃", "NASA太空中心附近有神秘修士出没", "德克萨斯大草原土系灵气异常浓郁"},
	"phoenix":        {"凤凰城沙漠中发现了失传的火系秘法", "仙人掌丛林中有神秘商人兜售稀有天材", "索诺兰沙漠深处有上古炼火宝地"},
	"san_francisco":  {"金门大桥两端各有一处水金阵法节点", "唐人街地下有华裔修士秘密传承", "硅谷创业公司掩护着一处修士据点"},
	"seattle":        {"西雅图常年阴雨滋养了一片水木双修圣地", "太空针塔顶封印着一只雨系妖兽", "瑞尼尔山山脚发现了水系矿脉"},
	"boston":         {"哈佛大学图书馆深处藏有古老修仙典籍", "波士顿港口水系灵气在独立战争期间被封印", "自由之路沿途有多处历史修士遗迹"},
	"las_vegas":      {"赌城赌场中暗藏一套金系概率阵法", "内华达沙漠边缘火系灵脉与金矿交织", "51区附近有神秘修士组织的据点"},
	"miami":          {"迈阿密海滩上有水火交融的天然炼器宝地", "大沼泽国家公园边缘有水系古修士遗迹", "南海滩地下暗藏一处火系炼器宝地"},
	"atlanta":        {"亚特兰大枢纽隐藏着一处木火双修秘境", "土地之下有内战遗留的修士战场遗迹", "桃树街底下木系灵脉绵延数里"},
	"detroit":        {"汽车厂房中封印着金系炼器大阵", "底特律河底有水系古修士洞府", "废弃工厂里金系灵气凝聚成块"},
	"minneapolis":    {"明尼苏达万湖之地水系灵气异常活跃", "密西西比河源头有水系大妖沉睡", "连锁湖泊构成天然水系阵法"},
	"st_louis":       {"密西西比河西岸土系灵气形成天然屏障", "拱门纪念碑封印着一处土系宝地", "路易斯安那购买土地中有古老土系传承"},
	"new_orleans":    {"新奥尔良沼泽地水木交融孕育奇异天材", "爵士音乐中隐藏着神秘的灵气波动规律", "法国区地下有伏都教修士留下的阵法"},
	"portland":       {"玫瑰园中木系灵气异常浓郁", "威拉米特河畔有水木双修圣地", "波特兰地下城隐藏着古修士遗迹"},
	"las_vegas":      {"赌城赌场中暗藏一套金系概率阵法", "内华达沙漠边缘火系灵脉与金矿交织", "51区附近有神秘修士组织的据点"},
	"salt_lake_city": {"盐湖净化之地土系灵气极为纯净", "摩门教圣殿地下有土系传承秘室", "大盐湖矿物质中蕴含珍稀天材"},
	"albuquerque":    {"热气球节汇聚大量土火灵气", "新墨西哥沙漠中有纳瓦霍族修士传承", "查科峡谷古城遗址封印着土系大法"},
	"austin":         {"德克萨斯首府创新之火激活木系灵脉", "奥斯汀蝙蝠桥下有木系妖兽聚集", "科罗拉多河畔木火双修圣地"},
	"nashville":      {"音乐城的歌声引动木系灵气共鸣", "大奥普里剧院地下有木系古修士遗迹", "康柏兰河畔木系灵药丰盛"},
	"charlotte":      {"南方金融之城木土相生形成独特灵脉", "夏洛特赛车道上有金系灵气凝聚", "卡特巴河两岸木系灵气茂盛"},
	"columbus":       {"大陆心脏土系灵气最为稳固", "俄亥俄州立大学隐藏着古老修仙学院", "科学博物馆地下有土系矿脉"},
	"indianapolis":   {"印第安纳波利斯赛道土金交叠形成特殊阵法", "怀特河畔土系灵气沉积丰厚", "500英里赛道的速度激发金系灵气"},
	"jacksonville":   {"圣约翰斯河水木交汇处有古修士遗迹", "杰克逊维尔港口水系灵气充沛", "佛罗里达北部水木双系天材丰盛"},
	"baltimore":      {"切萨皮克湾水系灵气汇聚成海", "巴尔的摩内港有水系古修士洞府", "联邦山上可感受到整片海湾的水系灵脉"},
}

// RollCityRealmRewards generates rewards for a city realm exploration with narrative seed
func RollCityRealmRewards(cr CityRealm) (map[string]int64, map[string]interface{}) {
	rewards := make(map[string]int64)
	// Base XP with some variance
	xpVariance := cr.BaseXP / 5
	rewards["cultivation_xp"] = cr.BaseXP + rand.Int63n(xpVariance*2+1) - xpVariance
	// Spirit stone
	stoneVariance := cr.BaseSpiritStone / 5
	rewards["spirit_stone"] = cr.BaseSpiritStone + rand.Int63n(stoneVariance*2+1) - stoneVariance
	// Materials per element
	for _, elem := range cr.Elements {
		qty := int64(cr.BaseMaterials) + rand.Int63n(int64(cr.BaseMaterials)+1)
		if qty > 0 {
			rewards["material_"+elem] = qty
		}
	}

	// Generate narrative seed
	encounterType := EncounterTypes[rand.Intn(len(EncounterTypes))]
	primaryElement := cr.Elements[0]
	hint := "在" + cr.Name + "探索中发现了神秘的灵气涌动"
	if hints, ok := CityNarrativeHints[cr.ID]; ok && len(hints) > 0 {
		hint = hints[rand.Intn(len(hints))]
	}

	// Build spirit materials list for narrative
	var matList []map[string]interface{}
	for k, qty := range rewards {
		if len(k) > 9 && k[:9] == "material_" {
			elem := k[9:]
			matList = append(matList, map[string]interface{}{
				"element": elem,
				"name":    ElementChinese(elem) + "系" + cr.Name + "灵晶",
				"qty":     qty,
			})
		}
	}

	durationYears := cr.DurationSec / int(GameYearDuration.Seconds())
	if durationYears < 1 {
		durationYears = 1
	}

	seed := map[string]interface{}{
		"location":       cr.Name,
		"element":        ElementChinese(primaryElement),
		"duration_years": durationYears,
		"encounter_type": encounterType,
		"drops": map[string]interface{}{
			"cultivation_xp": rewards["cultivation_xp"],
			"spirit_stone":   rewards["spirit_stone"],
			"spirit_material": matList,
		},
		"narrative_hint": hint,
	}
	return rewards, seed
}

// ========== 修为计算 ==========

const (
	// Base XP per 5-minute game year
	BaseXPPerYear            int64 = 15000 // ~500/turn * 30 turns-equivalent
	SoulSenseRecoveryPerYear int64 = 5
	DefaultSoulSenseMax      int64 = 100

	// Breakthrough
	BreakthroughBaseRate       = 70 // % for major realm transitions
	BreakthroughFailXPLossPct  = 10 // % of XP lost on failure
	BreakthroughFailLossChance = 30 // % chance of XP loss on failure
)

// CalcXPPerYear calculates XP gain per game year based on all bonuses
func CalcXPPerYear(rootMultiplier float64, raceID string, caveLevel int, equippedTech string) int64 {
	base := float64(BaseXPPerYear)

	// Spirit root multiplier
	base *= rootMultiplier

	// Race bonuses
	race, ok := Races[raceID]
	if ok {
		if race.CultivationSpeedPct > 0 {
			base *= 1.0 + float64(race.CultivationSpeedPct)/100.0
		}
		if race.IdleCultivationPct > 0 {
			base *= 1.0 + float64(race.IdleCultivationPct)/100.0
		}
	}

	// Cave bonus
	base *= 1.0 + CaveIdleBonus(caveLevel)

	// Technique bonus
	if equippedTech != "" {
		if tech, ok := Techniques[equippedTech]; ok {
			base *= 1.0 + tech.XPBonusPct/100.0
		}
	}

	return int64(base)
}

// BreakthroughResult holds the outcome of a breakthrough attempt
type BreakthroughResult struct {
	Success      bool
	NewRealm     string
	NewLevel     int
	XPConsumed   int64
	XPLost       int64
	ItemConsumed string
	Message      string
}

// RandBool returns a random true/false
func RandBool() bool {
	return rand.Intn(2) == 0
}
