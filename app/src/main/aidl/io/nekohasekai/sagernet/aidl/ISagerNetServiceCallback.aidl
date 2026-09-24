package io.nekohasekai.sagernet.aidl;

import io.nekohasekai.sagernet.aidl.SpeedDisplayData;
import io.nekohasekai.sagernet.aidl.TrafficDataBatch;

oneway interface ISagerNetServiceCallback {
  void stateChanged(int state, String profileName, String msg);
  void missingPlugin(String profileName, String pluginName);
  void cbSpeedUpdate(in SpeedDisplayData stats);
  void cbTrafficUpdate(in TrafficDataBatch stats);
  void cbSelectorUpdate(long id);
  void cbSpeedTestProgress(long profileId, String phase, double currentMBps, double peakMBps, long transferredBytes);
  void cbSpeedTestComplete(long profileId, double downloadMBps, double uploadMBps);
  void cbSpeedTestError(long profileId, String message);
}
