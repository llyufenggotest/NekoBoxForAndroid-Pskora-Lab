package moe.matsuri.nb4a.proxy.anytls;

import androidx.annotation.NonNull;
import com.esotericsoftware.kryo.io.ByteBufferInput;
import com.esotericsoftware.kryo.io.ByteBufferOutput;
import org.jetbrains.annotations.NotNull;
import io.nekohasekai.sagernet.fmt.AbstractBean;
import io.nekohasekai.sagernet.fmt.KryoConverters;

public class AnyTLSBean extends AbstractBean {

    public static final Creator<AnyTLSBean> CREATOR = new CREATOR<AnyTLSBean>() {
        @NonNull
        @Override
        public AnyTLSBean newInstance() {
            return new AnyTLSBean();
        }

        @Override
        public AnyTLSBean[] newArray(int size) {
            return new AnyTLSBean[size];
        }
    };
    
    public String password;
    public String sni;
    public String alpn;
    public String certificates;
    public String utlsFingerprint;
    public Boolean allowInsecure;
    public String echConfig;
    public String realityPubKey;
    public String realityShortId;

    // 🚀 新增的连接池参数
    public Integer minIdleSession;
    public Integer idleSessionCheckInterval;
    public Integer idleSessionTimeout;

    @Override
    public void initializeDefaultValues() {
        super.initializeDefaultValues();
        if (password == null) password = "";
        if (sni == null) sni = "";
        if (alpn == null) alpn = "";
        if (certificates == null) certificates = "";
        if (utlsFingerprint == null) utlsFingerprint = "";
        if (allowInsecure == null) allowInsecure = false;
        if (echConfig == null) echConfig = "";
        if (realityPubKey == null) realityPubKey = "";
        if (realityShortId == null) realityShortId = "";
    }

    @Override
    public void serialize(ByteBufferOutput output) {
        output.writeInt(2); // 🚀 升级版本号到 2
        super.serialize(output);
        output.writeString(password);
        output.writeString(sni);
        output.writeString(alpn);
        output.writeString(certificates);
        output.writeString(utlsFingerprint);
        output.writeBoolean(allowInsecure);
        output.writeString(echConfig);
        output.writeString(realityPubKey);
        output.writeString(realityShortId);
        
        // 🚀 保存新参数
        output.writeString(minIdleSession != null ? String.valueOf(minIdleSession) : "");
        output.writeString(idleSessionCheckInterval != null ? String.valueOf(idleSessionCheckInterval) : "");
        output.writeString(idleSessionTimeout != null ? String.valueOf(idleSessionTimeout) : "");
    }

    @Override
    public void deserialize(ByteBufferInput input) {
        int version = input.readInt();
        super.deserialize(input);
        password = input.readString();
        sni = input.readString();
        alpn = input.readString();
        certificates = input.readString();
        utlsFingerprint = input.readString();
        allowInsecure = input.readBoolean();
        echConfig = input.readString();
        if (version >= 1) {
            realityPubKey = input.readString();
            realityShortId = input.readString();
        } else {
            realityPubKey = "";
            realityShortId = "";
        }
        
        // 🚀 读取新参数 (兼顾旧版)
        if (version >= 2) {
            String mis = input.readString();
            minIdleSession = mis.isEmpty() ? null : Integer.parseInt(mis);
            String isci = input.readString();
            idleSessionCheckInterval = isci.isEmpty() ? null : Integer.parseInt(isci);
            String ist = input.readString();
            idleSessionTimeout = ist.isEmpty() ? null : Integer.parseInt(ist);
        } else {
            minIdleSession = null;
            idleSessionCheckInterval = null;
            idleSessionTimeout = null;
        }
    }

    @NotNull
    @Override
    public AnyTLSBean clone() {
        return KryoConverters.deserialize(new AnyTLSBean(), KryoConverters.serialize(this));
    }
}