package io.nekohasekai.sagernet.ui

import org.json.JSONArray
import org.json.JSONObject

/** Service-side exact config mapping. No name parsing and no group-delay fallback. */
internal fun mapPreferredRuntimeSamples(raw: JSONObject, members: Map<String, Long>) {
    val mapped = JSONArray()
    val samples = raw.optJSONArray("samples") ?: JSONArray()
    for (index in 0 until samples.length()) {
        val sample = samples.optJSONObject(index) ?: continue
        val id = members[sample.optString("tag")] ?: continue
        if (id <= 0 || sample.optString("source") != "auto_urltest" ||
            !sample.has("delay") || sample.optInt("delay", -1) < 0 || sample.optLong("sampleTime") <= 0) continue
        mapped.put(JSONObject().put("id", id).put("delay", sample.getInt("delay"))
            .put("sampleTime", sample.getLong("sampleTime")).put("source", "auto_urltest"))
    }
    raw.put("samples", mapped)
}

internal fun readPreferredAutomatic(value: JSONObject): Map<Long, PreferredAutoSample> {
    val result = mutableMapOf<Long, PreferredAutoSample>()
    val samples = value.optJSONArray("samples") ?: return result
    for (index in 0 until samples.length()) {
        val sample = samples.optJSONObject(index) ?: continue
        val id = sample.optLong("id")
        val time = sample.optLong("sampleTime")
        val delay = sample.optInt("delay", -1)
        if (id <= 0 || time <= 0 || delay < 0 || sample.optString("source") != "auto_urltest") continue
        if (time >= (result[id]?.sampleTime ?: 0)) result[id] = PreferredAutoSample(time, delay)
    }
    return result
}
