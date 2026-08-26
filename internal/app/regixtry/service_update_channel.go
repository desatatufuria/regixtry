package regixtry

import (
	"context"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// GetUpdateChannel resolves the global update-check channel with a
// code-level default fallback (mirrors GetSigningPolicySettings' own
// row-absence handling): a fresh install with zero rows ever written
// reports ports.UpdateChannelStable, the quiet/safe default.
func (s *Service) GetUpdateChannel(ctx context.Context) (string, error) {
	settings, err := s.metadata.GetUpdateChannel(ctx, s.tenant(ctx))
	if domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.UpdateChannelStable, nil
	}
	if err != nil {
		return "", err
	}
	return settings.Channel, nil
}

// SetUpdateChannel persists the global update-check channel, mirroring
// UpdateSigningPolicySettings' shape. Validation happens at the HTTP decode
// layer (mirroring decodeScanPolicySettings' own convention, not this
// service method) -- this is a thin, trusting pass-through consistent with
// every other UpdateXPolicySettings method in this file.
func (s *Service) SetUpdateChannel(ctx context.Context, channel string) (string, error) {
	settings := ports.UpdateChannelSettings{Channel: channel, UpdatedAt: s.now()}
	if err := s.metadata.UpsertUpdateChannel(ctx, s.tenant(ctx), settings); err != nil {
		return "", err
	}
	return channel, nil
}
