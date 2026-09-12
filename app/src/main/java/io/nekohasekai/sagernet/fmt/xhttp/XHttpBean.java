package io.nekohasekai.sagernet.fmt.xhttp;

import androidx.annotation.NonNull;

import com.esotericsoftware.kryo.io.ByteBufferInput;
import com.esotericsoftware.kryo.io.ByteBufferOutput;

import org.jetbrains.annotations.NotNull;

import io.nekohasekai.sagernet.fmt.AbstractBean;
import io.nekohasekai.sagernet.fmt.KryoConverters;
import moe.matsuri.nb4a.utils.JavaUtil;

public class XHttpBean extends AbstractBean {

    public String password; // 对应 YAML 中的 JWT Token
    public String nodeId;   // 对应 YAML 中的 node-id

    @Override
    public void initializeDefaultValues() {
        super.initializeDefaultValues();
        if (JavaUtil.isNullOrBlank(password)) password = "";
        if (JavaUtil.isNullOrBlank(nodeId)) nodeId = "";
    }

    @Override
    public void serialize(ByteBufferOutput output) {
        output.writeInt(1); // version
        super.serialize(output);
        output.writeString(password);
        output.writeString(nodeId);
    }

    @Override
    public void deserialize(ByteBufferInput input) {
        int version = input.readInt();
        super.deserialize(input);
        password = input.readString();
        nodeId = input.readString();
    }

    @NotNull
    @Override
    public XHttpBean clone() {
        return KryoConverters.deserialize(new XHttpBean(), KryoConverters.serialize(this));
    }

    public static final Creator<XHttpBean> CREATOR = new CREATOR<XHttpBean>() {
        @NonNull
        @Override
        public XHttpBean newInstance() {
            return new XHttpBean();
        }

        @Override
        public XHttpBean[] newArray(int size) {
            return new XHttpBean[size];
        }
    };
}