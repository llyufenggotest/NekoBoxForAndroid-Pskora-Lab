package io.nekohasekai.sagernet.fmt.snell;

import androidx.annotation.NonNull;

import com.esotericsoftware.kryo.io.ByteBufferInput;
import com.esotericsoftware.kryo.io.ByteBufferOutput;

import org.jetbrains.annotations.NotNull;

import io.nekohasekai.sagernet.fmt.AbstractBean;
import io.nekohasekai.sagernet.fmt.KryoConverters;

public class SnellBean extends AbstractBean {

    public String psk;
    public String userKey;
    public Integer version;      // 1-6
    public String obfsMode;      // "", "http", "tls"
    public String obfsHost;
    public String mode;          // v6: "", "default", "unshaped", "unsafe-raw"
    public Boolean quicProxyMode; // v6: legacy v5 QUIC Proxy compatibility
    public Boolean reuse;
    public String network;       // "tcp", "udp", "tcp,udp"

    // OIX raw ECH-TLS metadata. This layer only carries the values forward;
    // the underlying transport is intentionally not implemented here.
    public Boolean oixEchTls;
    public Integer oixIdentityVersion;
    public String oixAlpn;
    public Boolean oixLegacyFallback;
    public Integer oixPreconnect;
    public String oixSni;
    public String oixConfig;
    // Effective editor value plus source presence: absence uses exporter identity.
    public Boolean identity;
    public boolean identityPresent;
    public String oixPath;
    public Boolean oixSkipCertVerify;
    public Boolean tcpFastOpen;

    @Override
    public void initializeDefaultValues() {
        if (serverPort == null) serverPort = 443;
        if (version == null) version = 4;
        if (psk == null) psk = "";
        if (userKey == null) userKey = "";
        if (obfsMode == null) obfsMode = "";
        if (obfsHost == null) obfsHost = "";
        if (mode == null || mode.isEmpty()) mode = "default";
        if (quicProxyMode == null) quicProxyMode = false;
        if (reuse == null) reuse = false;
        if (network == null) network = "";
        if (oixEchTls == null) oixEchTls = false;
        if (oixIdentityVersion == null) oixIdentityVersion = 2;
        if (oixAlpn == null) oixAlpn = "snell-ech/1";
        if (oixLegacyFallback == null) oixLegacyFallback = false;
        if (oixPreconnect == null) oixPreconnect = 0;
        if (oixSni == null) oixSni = "";
        if (oixConfig == null) oixConfig = "";
        if (identity == null) identity = true;
        if (oixPath == null) oixPath = "";
        if (oixSkipCertVerify == null) oixSkipCertVerify = false;
        if (tcpFastOpen == null) tcpFastOpen = false;

        super.initializeDefaultValues();
    }

    @Override
    public void serialize(ByteBufferOutput output) {
        output.writeInt(6); // append-only Bean format; reads formats 1-5
        super.serialize(output);
        output.writeString(psk);
        output.writeInt(version);
        output.writeString(obfsMode);
        output.writeString(obfsHost);
        output.writeBoolean(reuse);
        output.writeString(network);
        output.writeString(userKey);
        output.writeString(mode);
        output.writeBoolean(quicProxyMode);
        output.writeBoolean(oixEchTls);
        output.writeInt(oixIdentityVersion);
        output.writeString(oixAlpn);
        output.writeBoolean(oixLegacyFallback);
        output.writeInt(oixPreconnect);
        output.writeString(oixSni);
        output.writeString(oixConfig);
        output.writeBoolean(identity);
        output.writeString(oixPath);
        output.writeBoolean(oixSkipCertVerify);
        output.writeBoolean(tcpFastOpen);
        // Format 6 is unreleased; append presence without altering formats 1-5.
        // A direct editor/API assignment of false is always explicit.
        output.writeBoolean(identityPresent || Boolean.FALSE.equals(identity));
    }

    @Override
    public void deserialize(ByteBufferInput input) {
        int version = input.readInt();
        super.deserialize(input);
        psk = input.readString();
        this.version = input.readInt();
        obfsMode = input.readString();
        obfsHost = input.readString();
        reuse = input.readBoolean();
        if (version >= 2) {
            network = input.readString();
        }
        if (version >= 3) {
            userKey = input.readString();
            mode = input.readString();
        }
        if (version >= 4) {
            quicProxyMode = input.readBoolean();
        }
        if (version >= 5) {
            oixEchTls = input.readBoolean();
            oixIdentityVersion = input.readInt();
            oixAlpn = input.readString();
            oixLegacyFallback = input.readBoolean();
            oixPreconnect = input.readInt();
            oixSni = input.readString();
            oixConfig = input.readString();
        }
        if (version >= 6) {
            identity = input.readBoolean();
            oixPath = input.readString();
            oixSkipCertVerify = input.readBoolean();
            tcpFastOpen = input.readBoolean();
            identityPresent = input.readBoolean();
        }
        initializeDefaultValues();
    }

    @NotNull
    @Override
    public SnellBean clone() {
        return KryoConverters.deserialize(new SnellBean(), KryoConverters.serialize(this));
    }

    public static final Creator<SnellBean> CREATOR = new CREATOR<SnellBean>() {
        @NonNull
        @Override
        public SnellBean newInstance() {
            return new SnellBean();
        }

        @Override
        public SnellBean[] newArray(int size) {
            return new SnellBean[size];
        }
    };

    @NotNull
    @Override
    public String displayName() {
        return name;
    }

    @NotNull
    @Override
    public String displayAddress() {
        return serverAddress;
    }
}
