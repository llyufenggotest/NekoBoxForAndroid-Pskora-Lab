package io.nekohasekai.sagernet.fmt.oppa;

import androidx.annotation.NonNull;

import com.esotericsoftware.kryo.io.ByteBufferInput;
import com.esotericsoftware.kryo.io.ByteBufferOutput;

import org.jetbrains.annotations.NotNull;

import io.nekohasekai.sagernet.fmt.AbstractBean;
import io.nekohasekai.sagernet.fmt.KryoConverters;

public class OppaBean extends AbstractBean {

    public String password;
    public Integer preConnect;
    public String sni;
    public Boolean allowInsecure;
    public String certificatePinSHA256;

    @Override
    public void initializeDefaultValues() {
        super.initializeDefaultValues();
        if (serverPort == null || serverPort == 1080) serverPort = 443;
        if (password == null) password = "";
        if (preConnect == null || preConnect <= 0) preConnect = 8;
        if (sni == null) sni = "";
        if (allowInsecure == null) allowInsecure = false;
        if (certificatePinSHA256 == null) certificatePinSHA256 = "";
    }

    @Override
    public void serialize(ByteBufferOutput output) {
        output.writeInt(1);
        super.serialize(output);
        output.writeString(password);
        output.writeInt(preConnect);
        output.writeString(sni);
        output.writeBoolean(allowInsecure);
        output.writeString(certificatePinSHA256);
    }

    @Override
    public void deserialize(ByteBufferInput input) {
        int version = input.readInt();
        super.deserialize(input);
        password = input.readString();
        preConnect = input.readInt();
        sni = input.readString();
        allowInsecure = input.readBoolean();
        certificatePinSHA256 = input.readString();
    }

    @NotNull
    @Override
    public OppaBean clone() {
        return KryoConverters.deserialize(new OppaBean(), KryoConverters.serialize(this));
    }

    public static final Creator<OppaBean> CREATOR = new CREATOR<OppaBean>() {
        @NonNull
        @Override
        public OppaBean newInstance() {
            return new OppaBean();
        }

        @Override
        public OppaBean[] newArray(int size) {
            return new OppaBean[size];
        }
    };
}
