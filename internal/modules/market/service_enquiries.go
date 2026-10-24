// Enquiries on market ads: chat, offers and closing.
package market

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

func (s *Service) SendEnquiry(ctx context.Context, actor ActorContext, adID int64, req CreateEnquiryRequest) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	if strings.TrimSpace(req.Message) == "" {
		return Enquiry{}, fmt.Errorf("%w: message is required", ErrInvalidInput)
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Enquiry{}, ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, adID)
	if err != nil {
		return Enquiry{}, err
	}
	if ad.Status != StatusPublished {
		return Enquiry{}, fmt.Errorf("%w: ad is not available", ErrInvalidInput)
	}
	if ad.NurseryID == nurseryID {
		return Enquiry{}, fmt.Errorf("%w: cannot enquire about your own ad", ErrInvalidInput)
	}
	existing, err := s.repo.EnquiryIDFor(ctx, adID, nurseryID)
	if err != nil {
		return Enquiry{}, err
	}
	if existing > 0 {
		return Enquiry{}, fmt.Errorf("%w: you have already sent an enquiry for this ad", ErrInvalidInput)
	}
	enquiry, err := s.repo.CreateEnquiry(ctx, adID, ad.NurseryID, nurseryID, actor.UserID, req)
	if err != nil {
		return Enquiry{}, err
	}
	return enrichEnquiry(enquiry, nurseryID), nil
}

func (s *Service) GetEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	if !s.canAccessEnquiry(actor, e, nurseryID) {
		return Enquiry{}, ErrForbidden
	}
	if nurseryID == e.AdNurseryID {
		_ = s.repo.MarkEnquiryViewed(ctx, id)
	}
	if nurseryID == e.AdNurseryID || nurseryID == e.EnquiringNurseryID {
		_ = s.repo.MarkChatRead(ctx, id, nurseryID)
	}
	msgs, _ := s.repo.GetMessages(ctx, id)
	e.Messages = msgs
	return enrichEnquiry(e, nurseryID), nil
}

func (s *Service) ListEnquiries(ctx context.Context, actor ActorContext, q EnquiriesQuery) ([]Enquiry, int, error) {
	if !canAccessMarket(actor) {
		return nil, 0, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return nil, 0, ErrForbidden
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 || q.PerPage > 50 {
		q.PerPage = 20
	}
	enquiries, total, err := s.repo.ListEnquiries(ctx, nurseryID, q)
	if err != nil {
		return nil, 0, err
	}
	if err := s.repo.ChatSummaries(ctx, enquiries, nurseryID); err != nil {
		return nil, 0, err
	}
	for i := range enquiries {
		enquiries[i] = enrichEnquiry(enquiries[i], nurseryID)
	}
	return enquiries, total, nil
}

// UnreadChats is the viewer's unread message count across open chats, for
// the app's Chats badge. Admins have no nursery and get 0.
func (s *Service) UnreadChats(ctx context.Context, actor ActorContext) int {
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return 0
	}
	n, _ := s.repo.UnreadTotal(ctx, nurseryID)
	return n
}

func (s *Service) ReplyToEnquiry(ctx context.Context, actor ActorContext, id int64, req ReplyEnquiryRequest) (Message, error) {
	if !canAccessMarket(actor) {
		return Message{}, ErrForbidden
	}
	if strings.TrimSpace(req.Body) == "" {
		return Message{}, fmt.Errorf("%w: body is required", ErrInvalidInput)
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Message{}, err
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Message{}, ErrForbidden
	}
	if !s.canAccessEnquiry(actor, e, nurseryID) {
		return Message{}, ErrForbidden
	}
	if reason := readOnlyReason(e); reason != "" {
		return Message{}, fmt.Errorf("%w: %s", ErrConflict, reason)
	}
	msg, err := s.repo.AddMessage(ctx, id, actor.UserID, nurseryID, req.Body)
	msg.IsMine = true
	return msg, err
}

// MakeOffer posts a price (and optional quantity) for the other nursery to
// accept or decline. Either side can offer, so the seller counters by making
// an offer back; a new offer withdraws the chat's previous pending one.
// MinOfferShare: a buyer can't offer less than this share of a for-sale ad's
// asking price. Lowball offers annoy sellers; the seller's counters are free.
const MinOfferShare = 0.7

// minOffer is the lowest price the enquiring nursery may offer, or 0 for no limit.
func minOffer(e Enquiry, offeringNurseryID int64) float64 {
	if offeringNurseryID != e.EnquiringNurseryID || e.AdType != AdTypeSell || e.AdPricePerUnit == nil || *e.AdPricePerUnit <= 0 {
		return 0
	}
	return math.Ceil(*e.AdPricePerUnit*MinOfferShare*100) / 100
}

func (s *Service) MakeOffer(ctx context.Context, actor ActorContext, id int64, req MakeOfferRequest) (Message, error) {
	if req.PricePerUnit <= 0 || req.PricePerUnit > 10_000_000 {
		return Message{}, fmt.Errorf("%w: offer price must be more than zero", ErrInvalidInput)
	}
	if req.Quantity != nil && *req.Quantity <= 0 {
		return Message{}, fmt.Errorf("%w: quantity must be more than zero", ErrInvalidInput)
	}
	e, nurseryID, err := s.writableEnquiry(ctx, actor, id)
	if err != nil {
		return Message{}, err
	}
	if e.Status == EnquiryDealAgreed {
		return Message{}, fmt.Errorf("%w: a deal is already agreed in this chat", ErrConflict)
	}
	if req.Quantity != nil && e.AdQuantity != nil && *req.Quantity > *e.AdQuantity {
		return Message{}, fmt.Errorf("%w: the ad has only %d available", ErrInvalidInput, *e.AdQuantity)
	}
	if low := minOffer(e, nurseryID); req.PricePerUnit < low {
		return Message{}, fmt.Errorf("%w: offer at least ₹%s — 70%% of the asking price", ErrInvalidInput, strconv.FormatFloat(low, 'f', -1, 64))
	}
	msg, err := s.repo.AddOffer(ctx, id, actor.UserID, nurseryID, req, "Offered "+dealText(req.PricePerUnit, req.Quantity, e.AdPriceUnit))
	msg.IsMine = true
	return msg, err
}

func (s *Service) AcceptOffer(ctx context.Context, actor ActorContext, id, messageID int64) (Enquiry, error) {
	return s.respondToOffer(ctx, actor, id, messageID, true)
}

func (s *Service) DeclineOffer(ctx context.Context, actor ActorContext, id, messageID int64) (Enquiry, error) {
	return s.respondToOffer(ctx, actor, id, messageID, false)
}

func (s *Service) respondToOffer(ctx context.Context, actor ActorContext, id, messageID int64, accept bool) (Enquiry, error) {
	e, nurseryID, err := s.writableEnquiry(ctx, actor, id)
	if err != nil {
		return Enquiry{}, err
	}
	dealBody := func(price float64, qty *int) string {
		return "Deal agreed: " + dealText(price, qty, e.AdPriceUnit)
	}
	if err := s.repo.RespondToOffer(ctx, id, messageID, actor.UserID, nurseryID, accept, dealBody); err != nil {
		return Enquiry{}, err
	}
	return s.GetEnquiry(ctx, actor, id)
}

// writableEnquiry loads a chat the actor's nursery is part of and refuses it
// when nobody can write to it any more.
func (s *Service) writableEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, int64, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, 0, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Enquiry{}, 0, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, 0, err
	}
	if nurseryID != e.AdNurseryID && nurseryID != e.EnquiringNurseryID {
		return Enquiry{}, 0, ErrForbidden
	}
	if reason := readOnlyReason(e); reason != "" {
		return Enquiry{}, 0, fmt.Errorf("%w: %s", ErrConflict, reason)
	}
	return e, nurseryID, nil
}

// readOnlyReason is empty while both nurseries can still write: the chat is
// open and its ad is live. Pausing, expiring or archiving the ad freezes it.
func readOnlyReason(e Enquiry) string {
	switch e.Status {
	case EnquiryClosed:
		return "the seller closed this chat"
	case EnquiryCancelled:
		return "the buyer cancelled this enquiry"
	}
	if e.AdStatus != StatusPublished {
		return "this ad is no longer active"
	}
	return ""
}

// dealText renders "₹120 per plant × 50"; the unit falls back to "each".
func dealText(price float64, qty *int, unit *string) string {
	per := "each"
	if unit != nil && strings.TrimSpace(*unit) != "" {
		per = strings.ToLower(strings.ReplaceAll(*unit, "_", " "))
	}
	text := "₹" + strconv.FormatFloat(price, 'f', -1, 64) + " " + per
	if qty != nil {
		text += fmt.Sprintf(" × %d", *qty)
	}
	return text
}

// CloseEnquiry lets the seller end a chat, e.g. after the pickup.
func (s *Service) CloseEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	return s.endEnquiry(ctx, actor, id, EnquiryClosed)
}

// CancelEnquiry lets the enquiring nursery withdraw.
func (s *Service) CancelEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	return s.endEnquiry(ctx, actor, id, EnquiryCancelled)
}

func (s *Service) endEnquiry(ctx context.Context, actor ActorContext, id int64, newStatus string) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	side := e.AdNurseryID // only the seller closes
	if newStatus == EnquiryCancelled {
		side = e.EnquiringNurseryID // only the buyer cancels
	}
	if nurseryID == 0 || nurseryID != side {
		return Enquiry{}, ErrForbidden
	}
	if e.Status == EnquiryClosed || e.Status == EnquiryCancelled {
		return Enquiry{}, fmt.Errorf("%w: this chat has already ended", ErrConflict)
	}
	if err := s.repo.SetEnquiryStatus(ctx, id, newStatus); err != nil {
		return Enquiry{}, err
	}
	return s.GetEnquiry(ctx, actor, id)
}

func (s *Service) requireOwner(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	ok, err := s.repo.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func (s *Service) canAccessEnquiry(actor ActorContext, e Enquiry, nurseryID int64) bool {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return true
	}
	return nurseryID == e.AdNurseryID || nurseryID == e.EnquiringNurseryID
}

func enrichEnquiry(enquiry Enquiry, actorNurseryID int64) Enquiry {
	status := strings.ToUpper(strings.TrimSpace(enquiry.Status))
	isBuyer := actorNurseryID > 0 && enquiry.EnquiringNurseryID == actorNurseryID
	isSeller := actorNurseryID > 0 && enquiry.AdNurseryID == actorNurseryID
	isOpen := status != EnquiryClosed && status != EnquiryCancelled
	readOnly := readOnlyReason(enquiry)
	canWrite := readOnly == "" && (isBuyer || isSeller)

	enquiry.Lifecycle = marketPtr(lifecycle.MarketEnquiry(status))
	enquiry.Summary = &EnquirySummary{
		IsBuyer:  isBuyer,
		IsSeller: isSeller,
		IsOpen:   isOpen,
	}
	enquiry.Capabilities = &EnquiryCapabilities{
		CanReply:       canWrite,
		CanOffer:       canWrite && status != EnquiryDealAgreed,
		CanClose:       isOpen && isSeller,
		CanCancel:      isOpen && isBuyer,
		ReadOnlyReason: readOnly,
	}
	for i := range enquiry.Messages {
		m := &enquiry.Messages[i]
		m.IsMine = actorNurseryID > 0 && m.SentByNurseryID == actorNurseryID
		m.CanRespond = canWrite && !m.IsMine && status != EnquiryDealAgreed &&
			m.OfferStatus != nil && *m.OfferStatus == OfferPending
	}
	return enquiry
}
