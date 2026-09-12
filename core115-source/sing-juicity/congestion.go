/*
Copyright (C) 2025 by dyhkwong

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/

package juicity

import (
	"context"

	"github.com/sagernet/quic-go"

	congestion_meta2 "github.com/sagernet/sing-quic/congestion_meta2"
)

func setCongestion(ctx context.Context, connection *quic.Conn, congestionName string) {

	// Although the official Juicity server can be configured to use `cubic`, `new_reno` and `bbr`, it is in fact a useless placebo option and BBR is always used.
	switch congestionName {
	case "bbr":
		fallthrough
	default:
		connection.SetCongestionControl(congestion_meta2.NewBbrSenderWithProfile(connection.InitialPacketSize(), congestion_meta2.ProfileStandard))
	}
}
