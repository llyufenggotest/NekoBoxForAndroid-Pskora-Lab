package io.nekohasekai.sagernet.aidl;

import io.nekohasekai.sagernet.aidl.ISagerNetServiceCallback;

interface ISagerNetService {
  int getState();
  String getProfileName();

  void registerCallback(in ISagerNetServiceCallback cb, int id);
  oneway void unregisterCallback(in ISagerNetServiceCallback cb);
  oneway void resetTraffic(in long[] profileIds);

  // Returns an error message, empty on success. Never starts a stopped session.
  String reconfigureLog(int level);
  int urlTest();
  String getPreferredSelection(long profileId);
  String queryIpQuality(long profileId);
  String startSpeedTest(long profileId, int streams);
  oneway void cancelSpeedTest();
}
