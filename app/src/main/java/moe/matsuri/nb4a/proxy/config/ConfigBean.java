package moe.matsuri.nb4a.proxy.config;

import androidx.annotation.NonNull;

import com.esotericsoftware.kryo.io.ByteBufferInput;
import com.esotericsoftware.kryo.io.ByteBufferOutput;
import com.google.gson.JsonObject;

import org.jetbrains.annotations.NotNull;

import io.nekohasekai.sagernet.fmt.KryoConverters;
import io.nekohasekai.sagernet.fmt.internal.InternalBean;
import moe.matsuri.nb4a.utils.JavaUtil;

public class ConfigBean extends InternalBean {

    public Integer type; // 0=config 1=outbound
    public String config;
    /** type=2 is a managed native urltest, not arbitrary JSON. */
    public java.util.List<Long> preferredMemberIds;
    public java.util.List<Long> preferredSourceGroupIds;
    public Integer preferredIntervalSeconds;
    public Integer preferredMinDelayMilliseconds;
    /** latency (legacy default) or stable. Appended in serialization v3. */
    public String preferredMode;
    /** v4: persistent source-ID exclusions for dynamic groups. */
    public java.util.List<Long> preferredExcludedMemberIds;

    @Override
    public void initializeDefaultValues() {
        super.initializeDefaultValues();
        if (type == null) type = 0;
        if (config == null) config = "";
        if (preferredMemberIds == null) preferredMemberIds = new java.util.ArrayList<>();
        if (preferredSourceGroupIds == null) preferredSourceGroupIds = new java.util.ArrayList<>();
        if (preferredIntervalSeconds == null) preferredIntervalSeconds = 300;
        if (preferredMinDelayMilliseconds == null) preferredMinDelayMilliseconds = 0;
        if (preferredMode == null) preferredMode = "latency";
        if (preferredExcludedMemberIds == null) preferredExcludedMemberIds = new java.util.ArrayList<>();
    }

    @Override
    public void serialize(ByteBufferOutput output) {
        initializeDefaultValues();
        output.writeInt(4);
        super.serialize(output);
        output.writeInt(type);
        output.writeString(config);
        output.writeInt(preferredMemberIds.size());
        for (Long id : preferredMemberIds) output.writeLong(id);
        output.writeInt(preferredIntervalSeconds);
        output.writeInt(preferredMinDelayMilliseconds);
        output.writeInt(preferredSourceGroupIds.size());
        for (Long id : preferredSourceGroupIds) output.writeLong(id);
        output.writeString(preferredMode);
        output.writeInt(preferredExcludedMemberIds.size());
        for (Long id : preferredExcludedMemberIds) output.writeLong(id);
    }

    @Override
    public void deserialize(ByteBufferInput input) {
        int version = input.readInt();
        super.deserialize(input);
        type = input.readInt();
        config = input.readString();
        preferredMemberIds = new java.util.ArrayList<>();
        preferredSourceGroupIds = new java.util.ArrayList<>();
        preferredIntervalSeconds = 300;
        preferredMinDelayMilliseconds = 0;
        preferredMode = "latency";
        if (version >= 1) {
            int memberCount = input.readInt();
            if (memberCount < 0 || memberCount > 100000) throw new IllegalArgumentException("Invalid preferred member count");
            for (int i = 0; i < memberCount; i++) preferredMemberIds.add(input.readLong());
            preferredIntervalSeconds = input.readInt();
            preferredMinDelayMilliseconds = input.readInt();
        }
        if (version >= 2) {
            int count = input.readInt();
            if (count < 0 || count > 100000) throw new IllegalArgumentException("Invalid preferred source count");
            for (int i = 0; i < count; i++) preferredSourceGroupIds.add(input.readLong());
        }
        if (version >= 3) preferredMode = input.readString();
        preferredExcludedMemberIds = new java.util.ArrayList<>();
        if (version >= 4) {
            int count = input.readInt();
            if (count < 0 || count > 100000) throw new IllegalArgumentException("Invalid preferred exclusion count");
            for (int i = 0; i < count; i++) preferredExcludedMemberIds.add(input.readLong());
        }
    }

    @Override
    public String displayName() {
        if (JavaUtil.isNotBlank(name)) {
            return name;
        } else {
            return "Custom " + Math.abs(hashCode());
        }
    }

    public String displayType() {
        if (type != null && type == 2) return "stable".equals(preferredMode) ? "优选分组（稳定备用）" : "优选分组（最低延迟）";
        if (type != null && type == 1 && JavaUtil.isNotBlank(config)) {
            try {
                JsonObject json = JavaUtil.gson.fromJson(config, JsonObject.class);
                if (json != null && json.has("type")) {
                    return json.get("type").getAsString() + " (sing-box)";
                }
            } catch (Exception ignored) {
            }
        }
        return type != null && type == 0 ? "sing-box config" : "sing-box outbound";
    }

    @NotNull
    @Override
    public ConfigBean clone() {
        return KryoConverters.deserialize(new ConfigBean(), KryoConverters.serialize(this));
    }

    public static final Creator<ConfigBean> CREATOR = new CREATOR<ConfigBean>() {
        @NonNull
        @Override
        public ConfigBean newInstance() {
            return new ConfigBean();
        }

        @Override
        public ConfigBean[] newArray(int size) {
            return new ConfigBean[size];
        }
    };
}