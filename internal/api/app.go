package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fiberws "github.com/gofiber/websocket/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/xiantu/server/internal/game"
	"github.com/xiantu/server/internal/ws"
)

func NewApp(pool *pgxpool.Pool, rdb *redis.Client, engine *game.Engine, jwtSecret string) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "仙途 Xiantu v0.3 · 黑人修仙传",
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))

	// Static files
	app.Static("/", "./public")

	h := NewHandler(pool, rdb, engine, jwtSecret)

	api := app.Group("/api")

	// ── Auth ──
	api.Post("/register", h.Register)
	api.Post("/login", h.Login)
	api.Get("/profile", h.AuthMiddleware, h.Profile)

	// ── Device login ──
	api.Post("/device-login/start", h.DeviceLoginStart)
	api.Post("/device-login/poll", h.DeviceLoginPoll)
	api.Post("/device-login/approve", h.AuthMiddleware, h.DeviceLoginApprove)
	api.Get("/device-login/pending", h.AuthMiddleware, h.DeviceLoginPending)

	// ── World (public) ──
	api.Get("/world/status", h.WorldStatus)
	api.Get("/world/tribulation", h.TribulationStatus)
	api.Post("/world/contribute", h.AuthMiddleware, h.Contribute)
	api.Get("/world/hall-of-fame", h.HallOfFame)

	// ── Game data (public) ──
	api.Get("/realms", h.Realms)
	api.Get("/races", h.RaceList)

	// ── Cultivation (authenticated) ──
	api.Post("/cultivate/offline", h.AuthMiddleware, h.OfflineCultivation)
	api.Post("/breakthrough", h.AuthMiddleware, h.Breakthrough)

	// ── Techniques ──
	api.Get("/techniques", h.TechniqueList)
	api.Post("/technique/equip", h.AuthMiddleware, h.EquipTechnique)

	// ── Secret realms (legacy type) ──
	api.Get("/secret-realms", h.SecretRealmList)
	api.Post("/secret-realm/explore", h.AuthMiddleware, h.ExploreSecretRealm)
	api.Get("/secret-realm/collect", h.AuthMiddleware, h.CollectExploration)

	// ── 洞府系统（美国景点，可占领）──
	api.Get("/caves", h.CaveList)
	api.Get("/caves/:id", h.CaveDetail)
	api.Post("/caves/:id/claim", h.AuthMiddleware, h.CaveClaim)
	api.Post("/caves/:id/challenge", h.AuthMiddleware, h.CaveChallenge)

	// ── 城市秘境系统（30个美国城市）──
	api.Get("/city-realms", h.OptionalAuth, h.CityRealmList)
	api.Post("/city-realms/:id/enter", h.AuthMiddleware, h.CityRealmEnter)
	api.Get("/city-realms/:id/status", h.AuthMiddleware, h.CityRealmStatus)
	api.Post("/city-realms/:id/exit", h.AuthMiddleware, h.CityRealmExit)

	// ── Alchemy ──
	api.Post("/alchemy/start", h.AuthMiddleware, h.StartAlchemy)
	api.Get("/alchemy/collect", h.AuthMiddleware, h.CollectAlchemy)

	// ── WebSocket ──
	hub := ws.NewHub(pool, rdb, engine, jwtSecret)
	go hub.Run()

	app.Use("/ws", func(c *fiber.Ctx) error {
		if fiberws.IsWebSocketUpgrade(c) {
			c.Locals("hub", hub)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws", fiberws.New(func(c *fiberws.Conn) {
		hub := c.Locals("hub").(*ws.Hub)
		hub.Handle(c)
	}))

	return app
}
