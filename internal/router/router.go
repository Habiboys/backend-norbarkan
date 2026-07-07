package router

import (
	"net/http"
	"path/filepath"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/handler"
	"backend-nobarkan/internal/middleware"
	"backend-nobarkan/internal/pkg/response"
	"backend-nobarkan/internal/repository"
	"backend-nobarkan/internal/service"
	ws "backend-nobarkan/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func New(db *gorm.DB, redisClient *redis.Client, cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())

	healthHandler := handler.NewHealthHandler(db, redisClient)
	streamHandler := handler.NewStreamHandler(cfg.Storage, cfg.JWT)
	webRTCHandler := handler.NewWebRTCHandler(cfg.WebRTC)
	cacheDir := filepath.Join(cfg.Storage.Path, "cache")
	proxyHandler := handler.NewProxyHandler(cacheDir)

	var authHandler *handler.AuthHandler
	var userHandler *handler.UserHandler
	var movieHandler *handler.MovieHandler
	var roomHandler *handler.RoomHandler
	var wsHandler *handler.WSHandler

	if db != nil {
		userRepo := repository.NewUserRepository(db)
		tokenRepo := repository.NewRefreshTokenRepository(db)
		movieRepo := repository.NewMovieRepository(db)
		roomRepo := repository.NewRoomRepository(db)
		memberRepo := repository.NewRoomMemberRepository(db)
		chatRepo := repository.NewChatRepository(db)

		authService := service.NewAuthService(userRepo, tokenRepo, cfg.JWT)
		userService := service.NewUserService(userRepo)
		sidecarClient := service.NewSidecarClient(cfg.Sidecar.URL)
		movieService := service.NewMovieService(movieRepo, cfg.Storage, sidecarClient)
		roomService := service.NewRoomService(roomRepo, memberRepo, chatRepo, cfg.JWT)

		authHandler = handler.NewAuthHandler(authService)
		userHandler = handler.NewUserHandler(userService)
		movieHandler = handler.NewMovieHandler(movieService)
		roomHandler = handler.NewRoomHandler(roomService)

		// Media source provider for proxy routes
		mediaSourceProvider := handler.NewMovieSourceProvider(movieRepo, sidecarClient)
		proxyRoutes := r.Group("")
		proxyRoutes.Use(func(c *gin.Context) {
			c.Set("mediaSourceProvider", mediaSourceProvider)
			c.Next()
		})
		{
			proxyRoutes.GET("/proxy/external/:movieId", proxyHandler.ExternalProxy)
			proxyRoutes.GET("/proxy/external/:movieId/status", proxyHandler.ExternalCacheStatus)
			proxyRoutes.GET("/proxy/external/:movieId/progress", proxyHandler.ExternalProgress)
		}

		wsHub := ws.NewHub()
		go wsHub.Run()
		wsHandler = handler.NewWSHandler(wsHub, chatRepo, userRepo, roomRepo, memberRepo, cfg.JWT.AccessSecret)
	}

	r.GET("/health", healthHandler.Check)
	r.GET("/stream/:movie_id/master.m3u8", streamHandler.Master)
	r.GET("/stream/:movie_id/:segment", streamHandler.Segment)

	// Legacy GDrive proxy - keep for backward compat but redirect or serve error
	r.GET("/proxy/drive/:fileId", func(c *gin.Context) {
		c.JSON(http.StatusGone, gin.H{
			"error": gin.H{
				"code":       "gdrive_deprecated",
				"title":      "Google Drive sudah tidak didukung",
				"message":    "Nobarkan sekarang menggunakan yt-dlp untuk semua sumber film. Gunakan menu Movies untuk menambah film via link.",
				"suggestion": "Buka halaman Movies, tambah film baru via link URL biasa.",
			},
		})
	})

	v1 := r.Group("/v1")
	{
		v1.GET("/ping", func(c *gin.Context) {
			response.OK(c, gin.H{"service": "nobarsync-api"}, "OK")
		})
		v1.GET("/webrtc/config", webRTCHandler.Config)

		if db == nil {
			unavailable := func(c *gin.Context) {
				response.Error(c, 503, "DATABASE_UNAVAILABLE", "Database belum terhubung. Server tetap hidup, tetapi fitur database belum tersedia.")
			}
			v1.Any("/ws", unavailable)
			v1.Any("/auth/*path", unavailable)
			v1.Any("/users/*path", unavailable)
			v1.Any("/movies", unavailable)
			v1.Any("/movies/*path", unavailable)
			v1.Any("/extractors", unavailable)
			v1.Any("/rooms", unavailable)
			v1.Any("/rooms/*path", unavailable)
			return r
		}

		v1.GET("/ws", func(c *gin.Context) {
			wsHandler.Serve(c.Writer, c.Request)
		})

		auth := v1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.Refresh)
			auth.POST("/logout", authHandler.Logout)
		}

		protected := v1.Group("")
		protected.Use(middleware.Auth(cfg.JWT.AccessSecret))
		{
			users := protected.Group("/users")
			{
				users.GET("/me", authHandler.Me)
				users.PUT("/me", userHandler.UpdateMe)
				users.PUT("/me/password", userHandler.ChangePassword)
			}

			movies := protected.Group("/movies")
			{
				movies.GET("", movieHandler.List)
				movies.POST("", movieHandler.CreateFromURL)
				movies.GET("/:id", movieHandler.Get)
				movies.PUT("/:id", movieHandler.Update)
				movies.DELETE("/:id", movieHandler.Delete)
				movies.GET("/:id/transcode-status", movieHandler.TranscodeStatus)
			}

			protected.GET("/extractors", movieHandler.ListExtractors)

			rooms := protected.Group("/rooms")
			{
				rooms.POST("", roomHandler.Create)
				rooms.GET("", roomHandler.List)
				rooms.GET("/my", roomHandler.MyRooms)
				rooms.GET("/:room", roomHandler.GetByCode)
				rooms.POST("/:room/join", roomHandler.Join)
				rooms.POST("/:room/leave", roomHandler.Leave)
				rooms.POST("/:room/close", roomHandler.Close)
				rooms.DELETE("/:room", roomHandler.Delete)
				rooms.PUT("/:room", roomHandler.Update)
				rooms.POST("/:room/chats", roomHandler.SendChat)
				rooms.GET("/:room/chats", roomHandler.Chats)
				rooms.POST("/:room/members/:user_id/kick", roomHandler.KickMember)
				rooms.POST("/:room/members/:user_id/mute", roomHandler.MuteMember)
				rooms.POST("/:room/members/:user_id/unmute", roomHandler.UnmuteMember)
			}
		}
	}

	return r
}
