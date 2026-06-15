package service

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/domain"
	passwordhash "backend-nobarkan/internal/pkg/bcrypt"
	jwtutil "backend-nobarkan/internal/pkg/jwt"
	"backend-nobarkan/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrRoomNotFound      = errors.New("room not found")
	ErrRoomForbidden     = errors.New("room forbidden")
	ErrRoomWrongPassword = errors.New("room wrong password")
	ErrRoomFull          = errors.New("room full")
	ErrRoomEnded         = errors.New("room ended")
)

// DriveCacheCleaner is a function type that deletes the local cache for a Drive fileID.
type DriveCacheCleaner func(fileID string)

type RoomService struct {
	rooms           *repository.RoomRepository
	members         *repository.RoomMemberRepository
	chats           *repository.ChatRepository
	jwtCfg          config.JWTConfig
	clearDriveCache DriveCacheCleaner
}

type CreateRoomInput struct {
	Name       string
	MovieID    *string
	Mode       string
	IsPrivate  bool
	Password   *string
	MaxMembers uint
	HostID     string
}

type UpdateRoomInput struct {
	Name       *string
	IsPrivate  *bool
	Password   *string
	MaxMembers *uint
}

type RoomResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Code        string               `json:"code"`
	Mode        domain.RoomMode      `json:"mode"`
	Status      domain.RoomStatus    `json:"status"`
	CurrentTime float64              `json:"current_time"`
	IsPlaying   bool                 `json:"is_playing"`
	IsPrivate   bool                 `json:"is_private"`
	MaxMembers  uint                 `json:"max_members"`
	MemberCount int64                `json:"member_count,omitempty"`
	Host        *UserResponse        `json:"host,omitempty"`
	Movie       *MovieResponse       `json:"movie,omitempty"`
	Members     []RoomMemberResponse `json:"members,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
}

type RoomMemberResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url,omitempty"`
	Role      string  `json:"role"`
	IsReady   bool    `json:"is_ready"`
	IsMuted   bool    `json:"is_muted"`
}

type JoinRoomResult struct {
	Room    RoomResponse `json:"room"`
	WSToken string       `json:"ws_token"`
}

type ChatResponse struct {
	ID        string          `json:"id"`
	User      *UserResponse   `json:"user,omitempty"`
	Message   string          `json:"message"`
	Type      domain.ChatType `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
}

type SendChatInput struct {
	Message string
	UserID  string
}

func NewRoomService(rooms *repository.RoomRepository, members *repository.RoomMemberRepository, chats *repository.ChatRepository, jwtCfg config.JWTConfig) *RoomService {
	return &RoomService{rooms: rooms, members: members, chats: chats, jwtCfg: jwtCfg}
}

// SetCacheCleaner sets the function used to clear Drive cache for a fileID.
func (s *RoomService) SetCacheCleaner(cleaner DriveCacheCleaner) {
	s.clearDriveCache = cleaner
}

func (s *RoomService) clearMovieDriveCache(room *domain.Room) {
	if s.clearDriveCache == nil || room == nil || room.Movie == nil || room.Movie.DriveFileID == nil || *room.Movie.DriveFileID == "" {
		return
	}
	s.clearDriveCache(*room.Movie.DriveFileID)
}

func (s *RoomService) Create(input CreateRoomInput) (*RoomResponse, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("invalid room name")
	}
	mode := domain.RoomMode(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = domain.RoomModeGDrive
	}
	if mode != domain.RoomModeGDrive {
		return nil, fmt.Errorf("invalid room mode")
	}
	maxMembers := input.MaxMembers
	if maxMembers == 0 {
		maxMembers = 10
	}

	var hashedPassword *string
	if input.IsPrivate {
		if input.Password == nil || strings.TrimSpace(*input.Password) == "" {
			return nil, fmt.Errorf("private room password required")
		}
		password := strings.TrimSpace(*input.Password)
		hashed, err := passwordhash.Hash(password)
		if err != nil {
			return nil, err
		}
		hashedPassword = &hashed
	}

	room := &domain.Room{
		ID:         uuid.NewString(),
		Name:       name,
		Code:       generateRoomCode(),
		HostID:     input.HostID,
		MovieID:    input.MovieID,
		Mode:       mode,
		Status:     domain.RoomStatusWaiting,
		IsPrivate:  input.IsPrivate,
		Password:   hashedPassword,
		MaxMembers: maxMembers,
	}
	if err := s.rooms.Create(room); err != nil {
		return nil, err
	}
	if err := s.members.Create(&domain.RoomMember{ID: uuid.NewString(), RoomID: room.ID, UserID: input.HostID, Role: domain.RoomRoleHost, IsReady: true, JoinedAt: time.Now()}); err != nil {
		return nil, err
	}
	created, err := s.rooms.FindByID(room.ID)
	if err != nil {
		return nil, err
	}
	response, err := s.toResponse(created, true)
	return &response, err
}

func (s *RoomService) List(filter repository.RoomListFilter) ([]RoomResponse, int64, int, int, error) {
	rooms, total, err := s.rooms.ListPublic(filter)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	items := make([]RoomResponse, 0, len(rooms))
	for i := range rooms {
		item, err := s.toResponse(&rooms[i], false)
		if err != nil {
			return nil, 0, 0, 0, err
		}
		items = append(items, item)
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 20
	}
	return items, total, filter.Page, filter.PerPage, nil
}

func (s *RoomService) GetByCode(code string) (*RoomResponse, error) {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, err
	}
	if room == nil {
		return nil, ErrRoomNotFound
	}
	response, err := s.toResponse(room, true)
	return &response, err
}

func (s *RoomService) Join(code string, userID string, password *string) (*JoinRoomResult, error) {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, err
	}
	if room == nil {
		return nil, ErrRoomNotFound
	}
	if room.Status == domain.RoomStatusEnded {
		return nil, ErrRoomEnded
	}
	isHost := room.HostID == userID
	existingMember, err := s.members.Find(room.ID, userID)
	if err != nil {
		return nil, err
	}
	alreadyActive := existingMember != nil && existingMember.LeftAt == nil
	if room.IsPrivate && !isHost && !alreadyActive {
		if password == nil || room.Password == nil || !passwordhash.Compare(*room.Password, strings.TrimSpace(*password)) {
			return nil, ErrRoomWrongPassword
		}
	}
	count, err := s.members.CountActive(room.ID)
	if err != nil {
		return nil, err
	}
	if uint(count) >= room.MaxMembers {
		member, err := s.members.Find(room.ID, userID)
		if err != nil {
			return nil, err
		}
		if member == nil || member.LeftAt != nil {
			return nil, ErrRoomFull
		}
	}

	role := domain.RoomRoleMember
	if isHost {
		role = domain.RoomRoleHost
	}
	if err := s.members.UpsertActive(&domain.RoomMember{ID: uuid.NewString(), RoomID: room.ID, UserID: userID, Role: role, JoinedAt: time.Now()}); err != nil {
		return nil, err
	}
	wsToken, err := jwtutil.Generate(userID, room.Code, s.jwtCfg.AccessSecret, s.jwtCfg.AccessExpired)
	if err != nil {
		return nil, err
	}
	response, err := s.toResponse(room, true)
	if err != nil {
		return nil, err
	}
	return &JoinRoomResult{Room: response, WSToken: wsToken}, nil
}

func (s *RoomService) Leave(code string, userID string) error {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return err
	}
	if room == nil {
		return ErrRoomNotFound
	}
	return s.members.Leave(room.ID, userID)
}

func (s *RoomService) Close(code string, userID string) error {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return err
	}
	if room == nil {
		return ErrRoomNotFound
	}
	if room.HostID != userID {
		return ErrRoomForbidden
	}
	if err := s.rooms.Close(room.ID); err != nil {
		return err
	}
	s.clearMovieDriveCache(room)
	return nil
}

func (s *RoomService) Delete(code string, userID string) error {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return err
	}
	if room == nil {
		return ErrRoomNotFound
	}
	if room.HostID != userID {
		return ErrRoomForbidden
	}
	if err := s.rooms.Delete(room.ID); err != nil {
		return err
	}
	s.clearMovieDriveCache(room)
	return nil
}

func (s *RoomService) Update(code string, userID string, input UpdateRoomInput) (*RoomResponse, error) {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, err
	}
	if room == nil {
		return nil, ErrRoomNotFound
	}
	if room.HostID != userID {
		return nil, ErrRoomForbidden
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		room.Name = strings.TrimSpace(*input.Name)
	}
	if input.IsPrivate != nil {
		room.IsPrivate = *input.IsPrivate
		if !*input.IsPrivate {
			room.Password = nil
		}
	}
	if input.Password != nil && strings.TrimSpace(*input.Password) != "" {
		hashed, err := passwordhash.Hash(*input.Password)
		if err != nil {
			return nil, err
		}
		room.Password = &hashed
	}
	if input.MaxMembers != nil && *input.MaxMembers > 0 {
		room.MaxMembers = *input.MaxMembers
	}
	if err := s.rooms.Update(room); err != nil {
		return nil, err
	}
	response, err := s.toResponse(room, true)
	return &response, err
}

func (s *RoomService) KickMember(code string, actorID string, targetUserID string) error {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return err
	}
	if room == nil {
		return ErrRoomNotFound
	}
	if room.HostID != actorID || targetUserID == "" || targetUserID == actorID || targetUserID == room.HostID {
		return ErrRoomForbidden
	}
	return s.members.Leave(room.ID, targetUserID)
}

func (s *RoomService) SetMemberMuted(code string, actorID string, targetUserID string, muted bool) error {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return err
	}
	if room == nil {
		return ErrRoomNotFound
	}
	if room.HostID != actorID || targetUserID == "" || targetUserID == actorID || targetUserID == room.HostID {
		return ErrRoomForbidden
	}
	return s.members.SetMuted(room.ID, targetUserID, muted)
}

func (s *RoomService) MyRooms(userID string) ([]RoomResponse, error) {
	rooms, err := s.rooms.FindByHostID(userID)
	if err != nil {
		return nil, err
	}
	items := make([]RoomResponse, 0, len(rooms))
	for i := range rooms {
		item, err := s.toResponse(&rooms[i], false)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *RoomService) SendChat(code string, input SendChatInput) (*ChatResponse, error) {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, err
	}
	if room == nil {
		return nil, ErrRoomNotFound
	}
	message := strings.TrimSpace(input.Message)
	if message == "" {
		return nil, fmt.Errorf("empty message")
	}
	chat := &domain.Chat{
		ID:        uuid.NewString(),
		RoomID:    room.ID,
		UserID:    input.UserID,
		Message:   message,
		Type:      domain.ChatTypeText,
		CreatedAt: time.Now(),
	}
	if err := s.chats.Create(chat); err != nil {
		return nil, err
	}
	user, err := s.members.Find(room.ID, input.UserID)
	if err != nil {
		return nil, err
	}
	var userResponse *UserResponse
	if user != nil && user.User != nil {
		value := toUserResponse(user.User)
		userResponse = &value
	}
	return &ChatResponse{ID: chat.ID, User: userResponse, Message: chat.Message, Type: chat.Type, CreatedAt: chat.CreatedAt}, nil
}

func (s *RoomService) Chats(code string, filter repository.ChatListFilter) ([]ChatResponse, int64, int, int, error) {
	room, err := s.rooms.FindByCode(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, 0, 0, 0, err
	}
	if room == nil {
		return nil, 0, 0, 0, ErrRoomNotFound
	}
	chats, total, err := s.chats.ListByRoom(room.ID, filter)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	items := make([]ChatResponse, 0, len(chats))
	for i := range chats {
		var user *UserResponse
		if chats[i].User != nil {
			value := toUserResponse(chats[i].User)
			user = &value
		}
		items = append(items, ChatResponse{ID: chats[i].ID, User: user, Message: chats[i].Message, Type: chats[i].Type, CreatedAt: chats[i].CreatedAt})
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 50
	}
	return items, total, filter.Page, filter.PerPage, nil
}

func (s *RoomService) toResponse(room *domain.Room, includeMembers bool) (RoomResponse, error) {
	var host *UserResponse
	if room.Host != nil {
		value := toUserResponse(room.Host)
		host = &value
	}
	var movie *MovieResponse
	if room.Movie != nil {
		m := room.Movie

		var drivePreviewURL *string
		if m.DriveFileID != nil && *m.DriveFileID != "" {
			value := "https://drive.google.com/file/d/" + *m.DriveFileID + "/preview"
			drivePreviewURL = &value
		}
		driveURL := m.DriveURL
		if driveURL == nil {
			driveURL = m.ExternalURL
		}

		var uploader *UserResponse
		if m.Uploader != nil {
			value := toUserResponse(m.Uploader)
			uploader = &value
		}

		var driveDirectURL *string
		if m.DriveFileID != nil && *m.DriveFileID != "" {
			value := "/proxy/drive/" + *m.DriveFileID
			driveDirectURL = &value
		}

		movie = &MovieResponse{
			ID:              m.ID,
			Title:           m.Title,
			Description:     m.Description,
			SourceType:      m.SourceType,
			ProviderName:    m.ProviderName,
			ExternalURL:     m.ExternalURL,
			DriveFileID:     m.DriveFileID,
			DriveURL:        driveURL,
			DrivePreviewURL: drivePreviewURL,
			DriveDirectURL:  driveDirectURL,
			ThumbnailURL:    m.ThumbnailURL,
			Duration:        m.Duration,
			FileSize:        m.FileSize,
			TranscodeStatus: m.TranscodeStatus,
			UploadedBy:      uploader,
			CreatedAt:       m.CreatedAt,
		}
	}
	count, err := s.members.CountActive(room.ID)
	if err != nil {
		return RoomResponse{}, err
	}
	response := RoomResponse{ID: room.ID, Name: room.Name, Code: room.Code, Mode: room.Mode, Status: room.Status, CurrentTime: room.CurrentTime, IsPlaying: room.IsPlaying, IsPrivate: room.IsPrivate, MaxMembers: room.MaxMembers, MemberCount: count, Host: host, Movie: movie, CreatedAt: room.CreatedAt}
	if includeMembers {
		members, err := s.members.ListActive(room.ID)
		if err != nil {
			return RoomResponse{}, err
		}
		response.Members = make([]RoomMemberResponse, 0, len(members))
		for i := range members {
			if members[i].User == nil {
				continue
			}
			response.Members = append(response.Members, RoomMemberResponse{ID: members[i].User.ID, Name: members[i].User.Name, AvatarURL: members[i].User.AvatarURL, Role: string(members[i].Role), IsReady: members[i].IsReady, IsMuted: members[i].IsMuted})
		}
	}
	return response, nil
}

func generateRoomCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	builder := strings.Builder{}
	for i := 0; i < 6; i++ {
		builder.WriteByte(chars[rand.Intn(len(chars))])
	}
	return builder.String()
}
