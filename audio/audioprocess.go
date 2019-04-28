// Copyright (c) 2019, The Emergent Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package audio

import (
	"fmt"
	"image"
	"math"
	"strconv"

	"github.com/chewxy/math32"
	"github.com/emer/dtable/dtable"
	"github.com/emer/dtable/etensor"
)

// AudInputSpec defines the sound input parameters for auditory processing
type AudInputSpec struct {
	WinMsec      float32 `desc:"#DEF_25 input window -- number of milliseconds worth of sound to filter at a time"`
	StepMsec     float32 `desc:"#DEF_5;10;12.5 input step -- number of milliseconds worth of sound that the input is stepped along to obtain the next window sample"`
	TrialMsec    float32 `desc:"#DEF_100 length of a full trial's worth of input -- total number of milliseconds to accumulate into a complete trial of activations to present to a network -- must be a multiple of step_msec -- input will be trial_msec / step_msec = trial_steps wide in the X axis, and number of filters in the Y axis"`
	BorderSteps  uint32  `desc:"number of steps before and after the trial window to preserve -- this is important when applying temporal filters that have greater temporal extent"`
	SampleRate   uint32  `desc:"rate of sampling in our sound input (e.g., 16000 = 16Khz) -- can initialize this from a taSound object using InitFromSound method"`
	Channels     uint32  `desc:"total number of channels to process"`
	Channel      uint32  `desc:"#CONDSHOW_ON_channels:1 specific channel to process, if input has multiple channels, and we only process one of them (-1 = process all)"`
	WinSamples   uint32  `desc:"#READ_ONLY #SHOW total number of samples to process (win_msec * .001 * sample_rate)"`
	StepSamples  uint32  `desc:"#READ_ONLY #SHOW total number of samples to step input by (step_msec * .001 * sample_rate)"`
	TrialSamples uint32  `desc:"#READ_ONLY #SHOW total number of samples in a trial  (trail_msec * .001 * sample_rate)"`
	TrialSteps   uint32  `desc:"#READ_ONLY #SHOW total number of steps in a trial  (trail_msec / step_msec)"`
	TotalSteps   uint32  `desc:"#READ_ONLY #SHOW 2*border_steps + trial_steps -- total in full window"`
}

//Init initializes the the AudInputSpec
func (ais *AudInputSpec) Initialize() {
	ais.WinMsec = 25.0
	ais.StepMsec = 5.0
	ais.TrialMsec = 100.0
	ais.BorderSteps = 12
	ais.SampleRate = 16000
	ais.Channels = 1
	ais.Channel = 0
	ais.ComputeSamples()
}

// ComputeSamples computes the sample counts based on time and sample rate
func (ais *AudInputSpec) ComputeSamples() {
	ais.WinSamples = MSecToSamples(ais.WinMsec, ais.SampleRate)
	ais.StepSamples = MSecToSamples(ais.StepMsec, ais.SampleRate)
	ais.TrialSamples = MSecToSamples(ais.TrialMsec, ais.SampleRate)
	ais.TrialSteps = uint32(math.Round(float64(ais.TrialMsec / ais.StepMsec)))
	ais.TotalSteps = 2*ais.BorderSteps + ais.TrialSteps
}

// MSecToSamples converts milliseconds to samples, in terms of sample_rate
func MSecToSamples(msec float32, rate uint32) uint32 {
	return uint32(math.Round(float64(msec) * 0.001 * float64(rate)))
}

// SamplesToMSec converts samples to milliseconds, in terms of sample_rate
func SamplesToMSec(samples int, rate int) float32 {
	return 1000.0 * float32(samples) / float32(rate)
}

// InitSound
func (ap *AuditoryProc) InitSound() bool {
	ap.InputPos = 0
	ap.SoundFull.Reset()
	return true;
}

// InitFromSound loads a sound and sets the AudInputSpec channel vars and sample rate
func (ais *AudInputSpec) InitFromSound(snd *Sound, nChannels uint32, channel uint32) {
	if snd == nil {
		fmt.Printf("InitFromSound: sound nil")
		return
	}

	ais.SampleRate = snd.SampleRate()
	ais.ComputeSamples()
	if nChannels < 1 {
		ais.Channels = uint32(snd.Channels())
	} else {
		ais.Channels = uint32(math32.Min(float32(nChannels), float32(ais.Channels)))
	}
	if ais.Channels > 1 {
		ais.Channel = channel
	} else {
		ais.Channel = 0
	}
}

// AudDftSpec discrete fourier transform (dft) specifications
type AudDftSpec struct {
	LogPow         bool    `desc:"#DEF_true compute the log of the power and save that to a separate table -- generaly more useful for visualization of power than raw power values"`
	LogOff         float32 `desc:"#CONDSHOW_ON_log_pow #DEF_0 add this amount when taking the log of the dft power -- e.g., 1.0 makes everything positive -- affects the relative contrast of the outputs"`
	LogMin         float32 `desc:"#CONDSHOW_ON_log_pow #DEF_-100 minimum value a log can produce -- puts a lower limit on log output"`
	PreviousSmooth float32 `desc:"#DEF_0 how much of the previous step's power value to include in this one -- smooths out the power spectrum which can be artificially bumpy due to discrete window samples"`
	CurrentSmooth  float32 `desc:"#READ_ONLY #EXPERT 1 - prv_smooth -- how much of current power to include"`
}

func (ad *AudDftSpec) Initialize() {
	ad.PreviousSmooth = 0
	ad.CurrentSmooth = 1.0 - ad.PreviousSmooth
	ad.LogPow = true
	ad.LogOff = 0
	ad.LogMin = -100
}

// AudRenormSpec holds the auditory renormalization parameters
type AudRenormSpec struct {
	On          bool    `desc:"perform renormalization of this level of the auditory signal"`
	RenormMin   float32 `desc:"#CONDSHOW_ON_on minimum value to use for renormalization -- you must experiment with range of inputs to determine appropriate values"`
	RenormMax   float32 `desc:"#CONDSHOW_ON_on maximum value to use for renormalization -- you must experiment with range of inputs to determine appropriate values"`
	RenormScale float32 `desc:"#READ_ONLY 1.0 / (ren_max - ren_min)"`
}

func (ar *AudRenormSpec) Initialize() {
	ar.On = true
	ar.RenormMin = -10.0
	ar.RenormMax = 7.0
	ar.RenormScale = 1.0 / (ar.RenormMax - ar.RenormMin)
}

// AudGaborSpec params for auditory gabor filters: 2d Gaussian envelope times a sinusoidal plane wave --
// by default produces 2 phase asymmetric edge detector filters -- horizontal tuning is different from V1 version --
// has elongated frequency-band specific tuning, not a parallel horizontal tuning -- and has multiple of these
type AudGaborSpec struct {
	On              bool    `desc:"use this gabor filtering of the time-frequency space filtered input (time in terms of steps of the DFT transform, and discrete frequency factors based on the FFT window and input sample rate)"`
	SizeTime        int     `desc:"#CONDSHOW_ON_on #DEF_6;8;12;16;24 size of the filter in the time (horizontal) domain, in terms of steps of the underlying DFT filtering steps"`
	SizeFreq        int     `desc:"#CONDSHOW_ON_on #DEF_6;8;12;16;24 size of the filter in the frequency domain, in terms of discrete frequency factors based on the FFT window and input sample rate"`
	SpaceTime       int     `desc:"#CONDSHOW_ON_on spacing in the time (horizontal) domain, in terms of steps"`
	SpaceFreq       int     `desc:"#CONDSHOW_ON_on spacing in the frequency (vertical) domain"`
	WaveLen         float32 `desc:"#CONDSHOW_ON_on #DEF_1.5;2 wavelength of the sine waves in normalized units"`
	SigmaLen        float32 `desc:"#CONDSHOW_ON_on #DEF_0.6 gaussian sigma for the length dimension (elongated axis perpendicular to the sine waves) -- normalized as a function of filter size in relevant dimension"`
	SigmaWidth      float32 `desc:"#CONDSHOW_ON_on #DEF_0.3 gaussian sigma for the width dimension (in the direction of the sine waves) -- normalized as a function of filter size in relevant dimension"`
	SigmaLenHoriz   float32 `desc:"#CONDSHOW_ON_on #DEF_0.3 gaussian sigma for the length of special horizontal narrow-band filters -- normalized as a function of filter size in relevant dimension"`
	SigmaWidthHoriz float32 `desc:"#CONDSHOW_ON_on #DEF_0.1 gaussian sigma for the horizontal dimension for special horizontal narrow-band filters -- normalized as a function of filter size in relevant dimension"`
	Gain            float32 `desc:"#CONDSHOW_ON_on #DEF_2 overall gain multiplier applied after gabor filtering -- only relevant if not using renormalization (otherwize it just gets renormed awaY"`
	NHoriz          int     `desc:"#CONDSHOW_ON_on #DEF_4 number of horizontally-elongated,  pure time-domain, frequency-band specific filters to include, evenly spaced over the available frequency space for this filter set -- in addition to these, there are two diagonals (45, 135) and a vertically-elongated (wide frequency band) filter"`
	PhaseOffset     float32 `desc:"#CONDSHOW_ON_on #DEF_0;1.5708 offset for the sine phase -- default is an asymmetric sine wave -- can make it into a symmetric cosine gabor by using PI/2 = 1.5708"`
	CircleEdge      bool    `desc:"#CONDSHOW_ON_on #DEF_true cut off the filter (to zero) outside a circle of diameter filter_size -- makes the filter more radially symmetric"`
	NFilters        int     `desc:"#CONDSHOW_ON_on #READ_ONLY #SHOW total number of filters = 3 + n_horiz"`
}

func (ag *AudGaborSpec) Initialize() {
	ag.On = true
	ag.Gain = 2.0
	ag.NHoriz = 4
	ag.SizeTime = 6.0
	ag.SizeFreq = 6.0
	ag.WaveLen = 1.5
	ag.SigmaLen = 0.6
	ag.SigmaWidth = 0.3
	ag.SigmaLenHoriz = 0.3
	ag.SigmaWidthHoriz = 0.1
	ag.PhaseOffset = 0.0
	ag.CircleEdge = true
	ag.NFilters = 3 + ag.NHoriz
}

// RenderFilters generates filters into the given matrix, which is formatted as: [sz_time_steps][sz_freq][n_filters]
func (ag *AudGaborSpec) RenderFilters(filters *etensor.Float32) {

}

// MelFBankSpec contains mel frequency feature bank sampling parameters
type MelFBankSpec struct {
	On       bool    `desc:"perform mel-frequency filtering of the fft input"`
	LoHz     float32 `desc:"#DEF_120;300 #CONDSHOW_ON_on low frequency end of mel frequency spectrum"`
	HiHz     float32 `desc:"#DEF_10000;8000 #CONDSHOW_ON_on high frequency end of mel frequency spectrum -- must be <= sample_rate / 2 (i.e., less than the Nyquist frequencY"`
	NFilters uint32  `desc:"#DEF_32;26 #CONDSHOW_ON_on number of Mel frequency filters to compute"`
	LogOff   float32 `desc:"#CONDSHOW_ON_on #DEF_0 on add this amount when taking the log of the Mel filter sums to produce the filter-bank output -- e.g., 1.0 makes everything positive -- affects the relative contrast of the outputs"`
	LogMin   float32 `desc:"#CONDSHOW_ON_on #DEF_-10 minimum value a log can produce -- puts a lower limit on log output"`
	LoMel    float32 `desc:"#READ_ONLY #SHOW #CONDSHOW_ON_on low end of mel scale in mel units"`
	HiMel    float32 `desc:"#READ_ONLY #SHOW #CONDSHOW_ON_on high end of mel scale in mel units"`
}

// FreqToMel converts frequency to mel scale
func FreqToMel(freq float32) float32 {
	return 1127.0 * math32.Log(1.0+freq/700.0)
}

// FreqToMel converts mel scale to frequency
func MelToFreq(mel float32) float32 {
	return 700.0 * (math32.Exp(mel/1127.0) - 1.0)
}

// FreqToBin converts frequency into FFT bin number, using parameters of number of FFT bins and sample rate
func FreqToBin(freq, nFft, sampleRate float32) int {
	return int(math32.Floor(((nFft + 1) * freq) / sampleRate))
}

func (mfb *MelFBankSpec) Initialize() {
	mfb.On = true
	mfb.LoHz = 120.0
	mfb.HiHz = 10000.0
	mfb.NFilters = 32
	mfb.LogOff = 0.0
	mfb.LogMin = -10.0
	mfb.LoMel = FreqToMel(mfb.LoHz)
	mfb.HiMel = FreqToMel(mfb.HiHz)
}

// MelCepstrumSpec holds the mel frequency sampling parameters
type MelCepstrumSpec struct {
	On     bool `desc:"perform cepstrum discrete cosine transform (dct) of the mel-frequency filter bank features"`
	NCoeff int  `desc:"#CONDSHOW_ON_on #DEF_13 number of mfcc coefficients to output -- typically 1/2 of the number of filterbank features"`
}

func (mc *MelCepstrumSpec) Initialize() {
	mc.On = true
	mc.NCoeff = 13
}

type AuditoryProc struct {
	// From C++ version not ported
	//	enum SaveMode {               // how to add new data to the data table
	//	NONE_SAVE,                  // don't save anything at all -- overrides any more specific save guys and prevents any addition or modification to the data table
	//	FIRST_ROW,                  // always overwrite the first row -- does EnforceRows(1) if rows = 0
	//	ADD_ROW,                    // always add a new row and write to that, preserving a history of inputs over time -- should be reset at some interval!
	//};
	//	SaveMode      save_mode;      // how to add new data to the data table
	//	V1KwtaSpec    gabor_kwta;     // #CONDSHOW_ON_gabor1.on k-winner-take-all inhibitory dynamics for the time-gabor output

	Data        *dtable.Table   `desc:"data table for saving filter results for viewing and applying to networks etc"`
	Input       AudInputSpec    `desc:"specifications of the raw auditory input"`
	Dft         AudDftSpec      `desc:"specifications for how to compute the discrete fourier transform (DFT, using FFT)"`
	MelFBank    MelFBankSpec    `desc:"specifications of the mel feature bank frequency sampling of the DFT (FFT) of the input sound"`
	FBankRenorm AudRenormSpec   `desc:"#CONDSHOW_ON_mel_fbank.on renormalization parmeters for the mel_fbank values -- performed prior to further processing"`
	Gabor1      AudGaborSpec    `desc:"#CONDSHOW_ON_mel_fbank.on full set of frequency / time gabor filters -- first size"`
	Gabor2      AudGaborSpec    `desc:"#CONDSHOW_ON_mel_fbank.on full set of frequency / time gabor filters -- second size"`
	Gabor3      AudGaborSpec    `desc:"#CONDSHOW_ON_mel_fbank.on full set of frequency / time gabor filters -- third size"`
	Mfcc        MelCepstrumSpec `desc:"#CONDSHOW_ON_mel_fbank.on specifications of the mel cepstrum discrete cosine transform of the mel fbank filter features"`

	// Filters
	DftSize        uint32 `desc:"#READ_ONLY #NO_SAVE full size of fft output -- should be input.win_samples"`
	DftUse         uint32 `desc:"#READ_ONLY #NO_SAVE number of dft outputs to actually use -- should be dft_size / 2 + 1"`
	MelNFiltersEff uint32 `desc:"#READ_ONLY #NO_SAVE effective number of mel filters: mel.n_filters + 2"`

	MelPtsMel        etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [mel_n_filters_eff] scale points in mel units (mels)"`
	MelPtsHz         etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [mel_n_filters_eff] mel scale points in hz units"`
	MelPtsBin        etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [mel_n_filters_eff] mel scale points in fft bins"`
	MelFilterMaxBins uint32          `desc:"#READ_ONLY #NO_SAVE maximum number of bins for mel filter -- number of bins in highest filter"`
	MelFilters       etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [mel_filt_max_bins][mel.n_filters] the actual filters for actual number of mel filters"`

	Gabor1Filters etensor.Float32 `desc:"#READ_ONLY #NO_SAVE full gabor filters"`
	Gabor2Filters etensor.Float32 `desc:"#READ_ONLY #NO_SAVE full gabor filters"`
	Gabor3Filters etensor.Float32 `desc:"#READ_ONLY #NO_SAVE full gabor filters"`

	// Outputs
	FirstStep     bool        `desc:"#READ_ONLY #NO_SAVE #SHOW is this the first step of processing -- turns of prv smoothing of dft power"`
	InputPos      uint32      `desc:"#READ_ONLY #NO_SAVE #SHOW current position in the sound_full input -- in terms of sample number"`
	TrialStartPos uint32      `desc:"#READ_ONLY #NO_SAVE #SHOW starting position of the current trial -- in terms of sample number"`
	TrialEndPos   uint32      `desc:"#READ_ONLY #NO_SAVE #SHOW ending position of the current trial -- in terms of sample number"`
	Gabor1Shape   image.Point `desc:"#CONDSHOW_ON_gabor1.on #READ_ONLY #SHOW overall geometry of gabor1 output (group-level geometry -- feature / unit level geometry is n_features, 2)"`
	Gabor2Shape   image.Point `desc:"#CONDSHOW_ON_gabor2.on #READ_ONLY #SHOW overall geometry of gabor2 output (group-level geometry -- feature / unit level geometry is n_features, 2)"`
	Gabor3Shape   image.Point `desc:"#CONDSHOW_ON_gabor3.on #READ_ONLY #SHOW overall geometry of gabor3 output (group-level geometry -- feature / unit level geometry is n_features, 2)"`

	SoundFull etensor.Float32 `desc:"#READ_ONLY #NO_SAVE the full sound input obtained from the sound input"`
	WindowIn  etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [input.win_samples] the raw sound input, one channel at a time"`

	DftOut etensor.ComplexFloat32 `desc:"#READ_ONLY #NO_SAVE [2, dft_size] discrete fourier transform (fft) output complex representation"`

	DftPowerOut         etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [dft_use] power of the dft, up to the nyquist limit frequency (1/2 input.win_samples)"`
	DftLogPowerOut      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [dft_use] log power of the dft, up to the nyquist limit frequency (1/2 input.win_samples)"`
	DftPowerTrialOut    etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [dft_use][input.total_steps][input.channels] full trial's worth of power of the dft, up to the nyquist limit frequency (1/2 input.win_samples)"`
	DftLogPowerTrialOut etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [dft_use][input.total_steps][input.channels] full trial's worth of log power of the dft, up to the nyquist limit frequency (1/2 input.win_samples)"`
	MelFBankOut         etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [mel.n_filters] mel scale transformation of dft_power, using triangular filters, resulting in the mel filterbank output -- the natural log of this is typically applied"`
	MelFBankTrialOut    etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [mel.n_filters][input.total_steps][input.channels] full trial's worth of mel feature-bank output -- only if using gabors"`
	GaborGci            etensor.Float32 `desc:"#READ_ONLY #NO_SAVE inhibitory conductances, for computing kwta"`
	Gabor1TrialRaw      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [gabor.n_filters*2][mel.n_filters][input.trial_steps][input.channels] raw output of gabor1 -- full trial's worth of gabor steps"`
	Gabor1TrialOut      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [gabor.n_filters*2][mel.n_filters][input.trial_steps][input.channels] post-kwta output of full trial's worth of gabor steps"`
	Gabor2TrialRaw      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [gabor.n_filters*2][mel.n_filters][input.trial_steps][input.channels] raw output of gabor1 -- full trial's worth of gabor steps"`
	Gabor2TrialOut      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [gabor.n_filters*2][mel.n_filters][input.trial_steps][input.channels] post-kwta output of full trial's worth of gabor steps"`
	Gabor3TrialRaw      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [gabor.n_filters*2][mel.n_filters][input.trial_steps][input.channels] raw output of gabor1 -- full trial's worth of gabor steps"`
	Gabor3TrialOut      etensor.Float32 `desc:"#READ_ONLY #NO_SAVE [gabor.n_filters*2][mel.n_filters][input.trial_steps][input.channels] post-kwta output of full trial's worth of gabor steps"`
	MfccDctOut          etensor.Float32 `desc:"#READ_ONLY #NO_SAVE discrete cosine transform of the log_mel_filter_out values, producing the final mel-frequency cepstral coefficients"`
	MfccDctTrialOut     etensor.Float32 `desc:"#READ_ONLY #NO_SAVE full trial's worth of discrete cosine transform of the log_mel_filter_out values, producing the final mel-frequency cepstral coefficients"`
}

// InitFilters
func (ap *AuditoryProc) InitFilters() bool {
	ap.DftSize = ap.Input.WinSamples
	ap.DftUse = ap.DftSize/2 + 1
	ap.InitFiltersMel()
	if ap.Gabor1.On {
		ap.Gabor1.RenderFilters(&ap.Gabor1Filters)
	}
	if ap.Gabor2.On {
		ap.Gabor2.RenderFilters(&ap.Gabor2Filters)
	}
	if ap.Gabor3.On {
		ap.Gabor3.RenderFilters(&ap.Gabor3Filters)
	}
	return true;
}

// InitFiltersMel
func (ap *AuditoryProc) InitFiltersMel() bool {
	ap.MelNFiltersEff = ap.MelFBank.NFilters + 2
	ap.MelPtsMel.SetShape([]int{1, int(ap.MelNFiltersEff)}, nil, nil)
	ap.MelPtsHz.SetShape([]int{1, int(ap.MelNFiltersEff)}, nil, nil)
	ap.MelPtsBin.SetShape([]int{1, int(ap.MelNFiltersEff)}, nil, nil)

	melIncr := (ap.MelFBank.HiMel - ap.MelFBank.LoMel) / float32(ap.MelFBank.NFilters+1)

	for idx := 0; idx < int(ap.MelNFiltersEff); idx++ {
		ml := ap.MelFBank.LoMel + float32(idx)*melIncr
		hz := MelToFreq(ml)
		bin := FreqToBin(hz, float32(ap.DftUse), float32(ap.Input.SampleRate))
		ap.MelPtsMel.SetFloat1D(idx, float64(ml))
		ap.MelPtsHz.SetFloat1D(idx, float64(hz))
		ap.MelPtsBin.SetFloat1D(idx, float64(bin))
	}

	ap.MelFilterMaxBins = uint32(ap.MelPtsBin.FloatVal1D(int(ap.MelNFiltersEff)-1)-ap.MelPtsBin.FloatVal1D(int(ap.MelNFiltersEff)-3)) + 1
	ap.MelFilters.SetShape([]int{int(ap.MelFilterMaxBins), int(ap.MelFBank.NFilters)}, nil, nil)

	for f := 0; f < int(ap.MelFBank.NFilters); f++ {
		mnbin := int(ap.MelPtsBin.Value1D(f))
		pkbin := int(ap.MelPtsBin.Value1D(f + 1))
		mxbin := int(ap.MelPtsBin.Value1D(f + 2))
		pkmin := pkbin - mnbin
		pkmax := mxbin - pkbin

		fi := 0
		bin := 0
		for bin = mnbin; bin < pkbin; bin, fi = bin+1, fi+1 {
			fval := float32((bin - mnbin) / pkmin)
			ap.MelFilters.SetFloat([]int{fi, f}, float64(fval))
		}
		for ; bin < mxbin; bin, fi = bin+1, fi+1 {
			fval := float32((mxbin - bin) / pkmax)
			ap.MelFilters.SetFloat([]int{fi, f}, float64(fval))
		}
	}
	return true;
}

// InitOutMatrix
func (ap *AuditoryProc) InitOutMatrix() bool {
	ap.WindowIn.SetShape64([]int64{int64(ap.Input.WinSamples)}, nil, nil)
	ap.DftOut.SetShape64([]int64{2, int64(ap.DftSize)}, nil, nil)
	ap.DftPowerOut.SetShape64([]int64{int64(ap.DftUse)}, nil, nil)
	ap.DftPowerTrialOut.SetShape64([]int64{int64(ap.DftUse), int64(ap.Input.TotalSteps), int64(ap.Input.Channels)}, nil, nil)

	if ap.Dft.LogPow {
		ap.DftLogPowerOut.SetShape64([]int64{int64(ap.DftUse)}, nil, nil)
		ap.DftLogPowerTrialOut.SetShape64([]int64{int64(ap.DftUse), int64(ap.Input.TotalSteps), int64(ap.Input.Channels)}, nil, nil)
	}

	if ap.MelFBank.On {
		ap.MelFBankOut.SetShape64([]int64{int64(ap.MelFBank.NFilters)}, nil, nil)
		ap.MelFBankTrialOut.SetShape64([]int64{int64(ap.MelFBank.NFilters), int64(ap.Input.TotalSteps), int64(ap.Input.Channels)}, nil, nil)

		if ap.MelFBank.On {
			if ap.Gabor1.On {
				ap.Gabor1TrialRaw.SetShape64([]int64{int64(ap.Gabor1.NFilters), 2, int64(ap.Gabor1Shape.Y), int64(ap.Gabor1Shape.X), int64(ap.Input.Channels)}, nil, nil)
				ap.Gabor1TrialOut.SetShape64([]int64{int64(ap.Gabor1.NFilters), 2, int64(ap.Gabor1Shape.Y), int64(ap.Gabor1Shape.X), int64(ap.Input.Channels)}, nil, nil)
			}
			if ap.Gabor2.On {
				ap.Gabor2TrialRaw.SetShape64([]int64{int64(ap.Gabor2.NFilters), 2, int64(ap.Gabor2Shape.Y), int64(ap.Gabor2Shape.X), int64(ap.Input.Channels)}, nil, nil)
				ap.Gabor2TrialOut.SetShape64([]int64{int64(ap.Gabor2.NFilters), 2, int64(ap.Gabor2Shape.Y), int64(ap.Gabor2Shape.X), int64(ap.Input.Channels)}, nil, nil)
			}
			if ap.Gabor3.On {
				ap.Gabor3TrialRaw.SetShape64([]int64{int64(ap.Gabor3.NFilters), 2, int64(ap.Gabor3Shape.Y), int64(ap.Gabor3Shape.X), int64(ap.Input.Channels)}, nil, nil)
				ap.Gabor3TrialOut.SetShape64([]int64{int64(ap.Gabor3.NFilters), 2, int64(ap.Gabor3Shape.Y), int64(ap.Gabor3Shape.X), int64(ap.Input.Channels)}, nil, nil)
			}
			if ap.Mfcc.On {
				ap.MfccDctOut.SetShape64([]int64{int64(ap.MelFBank.NFilters)}, nil, nil)
				ap.MfccDctTrialOut.SetShape64([]int64{int64(ap.MelFBank.NFilters), int64(ap.Input.TotalSteps), int64(ap.Input.Channels)}, nil, nil)
			}
		}
	}
	return true;
}

func (ap *AuditoryProc) LoadSound(snd *Sound) bool {
	var needsInit = false
	if ap.NeedsInit() {
		needsInit = true
	}

	if snd == nil || !snd.IsValid() {
		fmt.Printf("LoadSound: sound nil or invalid")
		return false
	}

	if snd.SampleRate() != ap.Input.SampleRate {
		fmt.Printf("LoadSound: sample rate does not match sound -- re-initializing with new rate of: %v", strconv.Itoa(int(snd.SampleRate())))
		ap.Input.SampleRate = snd.SampleRate()
		needsInit = true
	}

	if needsInit {
		ap.Init()
	}

	if ap.Input.Channels > 1 {
		snd.SoundToMatrix(&ap.SoundFull, -1)
	} else {
		snd.SoundToMatrix(&ap.SoundFull, ap.Input.Channel)
	}
	ap.StartNewSound()
	return true
}

func (ap *AuditoryProc) StartNewSound() bool {
	ap.FirstStep = true
	ap.InputPos = 0
	ap.TrialStartPos = 0
	ap.TrialEndPos = uint32(ap.TrialStartPos) + ap.Input.TrialSamples
	return true
}

func (ap *AuditoryProc) NeedsInit() bool {
	if uint32(ap.DftSize) != ap.Input.WinSamples || ap.MelNFiltersEff != ap.MelFBank.NFilters+2 {
		return true
	}
	return false

}

func (ap *AuditoryProc) Init() bool {
	ap.UpdateConfig()
	ap.InitFilters()
	ap.InitOutMatrix()
	ap.Data = &dtable.Table{}
	ap.InitDataTable()
	ap.InitSound()
	return true
}

func (ap *AuditoryProc) InitDataTable() bool {
	if ap.Data == nil {
		fmt.Printf("InitDataTable: ap.Data is nil")
		return false
	}
	if ap.Input.Channels > 1 {
		for ch := 0; ch < int(ap.Input.Channels); ch++ {
			ap.InitDataTableChan(ch)
		}
	} else {
		ap.InitDataTableChan(int(ap.Input.Channel))
	}
	return true
}

func (ap *AuditoryProc) InitDataTableChan(ch int) bool {
	if ap.MelFBank.On {
		ap.MelOutputToTable(ap.Data, ch, true)
	}
	return true
}

// InputStepsLeft returns the number of steps left to process in the current input sound
func (ap *AuditoryProc) InputStepsLeft() uint32 {
	samplesLeft := uint32(ap.SoundFull.NumDims()) - ap.InputPos
	//samplesLeft = ap.SoundFull.Frames() - ap.InputPos
	return samplesLeft / ap.Input.StepSamples
}

// ProcessTrial processes a full trial worth of sound -- iterates over steps to fill a trial's worth of sound data
func (ap *AuditoryProc) ProcessTrial() bool {
	if ap.NeedsInit() {
		ap.Init()
	}
	ap.Data.AddRows(1)

	if ap.InputStepsLeft() < 1 {
		fmt.Printf("ProcessTrial: no steps worth of input sound available -- load a new sound")
		return false
	}

	startPos := ap.InputPos
	if ap.InputPos == 0 { // just starting out -- fill whole buffer..
		border := 2 * ap.Input.BorderSteps // full amount to wrap
		ap.TrialStartPos = ap.InputPos
		ap.TrialEndPos = ap.TrialStartPos + ap.Input.TrialSamples + 2*border*ap.Input.StepSamples

		for ch := 0; ch < int(ap.Input.Channels); ch++ {
			ap.InputPos = startPos // always start at same place per channel
			for s := 0; s < int(ap.Input.TotalSteps); s++ {
				ap.ProcessStep(ch, s)
			}
			ap.FilterTrial(ch)
			ap.OutputToTable(ch)
		}
	} else {
		border := 2 * ap.Input.BorderSteps // full amount to wrap
		ap.TrialStartPos = ap.InputPos - ap.Input.TrialSamples*ap.Input.BorderSteps
		ap.TrialEndPos = ap.TrialStartPos + ap.Input.TrialSamples

		for ch := 0; ch < int(ap.Input.Channels); ch++ {
			ap.InputPos = startPos // always start at same place per channel
			ap.WrapBorder(ch)
			for s := border; s < ap.Input.TotalSteps; s++ {
				ap.ProcessStep(uint32(ch), s)
			}
			ap.FilterTrial(ch)
			ap.OutputToTable(ch)
		}
	}
	return true
}

// SoundToWindow gets sound from sound_full at given position and channel, into window_in -- pads with zeros for any amount not available in the sound_full input
func (ap *AuditoryProc) SoundToWindow(inPos uint32, ch uint32) bool {
	//int samp_avail = sound_full.Frames() - in_pos;
	//int samp_cpy = MIN(samp_avail, input.win_samples);
	//
	//if(samp_cpy > 0) {
	//	int sz = samp_cpy * sizeof(float);
	//	if(sound_full.dims() == 1) {
	//		memcpy(window_in.el, sound_full.el + in_pos, sz);
	//	}
	//	else {
	//		// todo: this is not right:
	//		memcpy(window_in.el, (void*)&(sound_full.FastEl2d(chan, in_pos)), sz);
	//	}
	//}
	//
	//samp_cpy = MAX(samp_cpy, 0);  // prevent negatives here -- otherwise overflows
	//// pad remainder with zero
	//int zero_n = input.win_samples - samp_cpy;
	//if(zero_n > 0) {
	//	int sz = zero_n * sizeof(float);
	//	memset(window_in.el + samp_cpy, 0, sz);
	//}
	//
	//available := ap.SoundFull.Frames() - inPos
	//sampleCopy := math32.Min(available, float32(ap.Input.WinSamples))
	//if sampleCopy > 0 {
	//	sz := sampleCopy * 4 // todo: this was sizeof(float) in c++ version
	//	if ap.SoundFull.NumDims() == 1 {
	//
	//	}
	//
	//}
	//
	//
	//

	return true

}

//bool AuditoryProc::StepToSample(int samp_pos) {
//if(NeedsInit()) Init();
//NewTableRow();
//if(TestError(InputStepsLeft() < 1, "StepToSample",
//"no steps worth of input sound available -- load a new sound")) {
//return false;
//}
//if(input_pos == 0)            // first process a trial if nothing loaded yet
//ProcessTrial();
//
//if(TestError(samp_pos < trial_start_pos, "StepToSample",
//"target sample position is earlier than current trial start position!")) {
//return false;
//}
//int steps_fwd = (samp_pos - trial_start_pos) / input.step_samples;
//
//int end_step = input.total_steps-1;
//
//for(int stp = 0; stp < steps_fwd; stp++) {
//int st_in_pos = input_pos;
//trial_start_pos += input.step_samples;
//trial_end_pos += input.step_samples;
//for(int chan=0; chan < input.channels; chan++) {
//input_pos = st_in_pos;      // always start at same place per channel
//StepForward(chan);
//ProcessStep(chan, end_step);
//FilterTrial(chan);
//}
//}
//
//for(int chan=0; chan < input.channels; chan++) {
//OutputToTable(chan);
//}
//return true;
//}
//
//bool AuditoryProc::WrapBorder(int chan) {
//if(input.border_steps == 0) return true;
//int bord_eff = 2 * input.border_steps; // full amount to wrap
//int src_st_step = input.total_steps - bord_eff;
//for(int step=0; step < bord_eff; step++) {
//CopyStepFromStep(step, src_st_step + step, chan);
//}
//return true;
//}
//
//bool AuditoryProc::StepForward(int chan) {
//int tot_m1 = input.total_steps - 1;
//for(int step=0; step < tot_m1; step++) {
//CopyStepFromStep(step, step+1, chan);
//}
//return true;
//}
//
//bool AuditoryProc::CopyStepFromStep(int to_step, int fm_step, int chan) {
//for(int i=0; i<dft_use; i++) {
//dft_power_trial_out.FastEl3d(i, to_step, chan) =
//dft_power_trial_out.FastEl3d(i, fm_step, chan);
//if(dft.log_pow) {
//dft_log_power_trial_out.FastEl3d(i, to_step, chan) =
//dft_log_power_trial_out.FastEl3d(i, fm_step, chan);
//}
//}
//if(mel_fbank.on) {
//for(int i=0; i < mel_fbank.n_filters; i++) {
//mel_fbank_trial_out.FastEl3d(i, to_step, chan) =
//mel_fbank_trial_out.FastEl3d(i, fm_step, chan);
//}
//if(mfcc.on) {
//for(int i=0; i < mel_fbank.n_filters; i++) {
//mfcc_dct_trial_out.FastEl3d(i, to_step, chan) =
//mfcc_dct_trial_out.FastEl3d(i, fm_step, chan);
//}
//}
//}
//return true;
//}

// ProcessStep process a step worth of sound input from current input_pos, and increment input_pos by input.step_samples
func (ap *AuditoryProc) ProcessStep(ch uint32, step uint32) bool {
	ap.SoundToWindow(ap.InputPos, ch)
	ap.FilterWindow(int(ch), int(step))
	ap.InputPos = ap.InputPos + ap.Input.StepSamples
	ap.FirstStep = false
	return true
}

// DftInput applies dft (fft) to input
func (ap *AuditoryProc) DftInput(ch int, step int) {
	//taMath_float::fft_real(&dft_out, &window_in);
}

// FilterTrial process filters that operate over an entire trial at a time
func (ap *AuditoryProc) FilterTrial(ch int) bool {
	if ap.Gabor1.On {
		ap.GaborFilter(ch, &ap.Gabor1, &ap.Gabor1Filters,
			&ap.Gabor1TrialRaw, &ap.Gabor1TrialOut)
	}
	if ap.Gabor2.On {
		ap.GaborFilter(ch, &ap.Gabor2, &ap.Gabor2Filters,
			&ap.Gabor2TrialRaw, &ap.Gabor2TrialOut)
	}
	if ap.Gabor3.On {
		ap.GaborFilter(ch, &ap.Gabor3, &ap.Gabor3Filters,
			&ap.Gabor3TrialRaw, &ap.Gabor3TrialOut)
	}
	return true
}

// GaborFilter process filters that operate over an entire trial at a time
func (ap *AuditoryProc) GaborFilter(ch int, spec *AudGaborSpec, filters *etensor.Float32, outRaw *etensor.Float32, out *etensor.Float32) {
	tHalfSz := spec.SizeTime / 2
	tOff := tHalfSz - int(ap.Input.BorderSteps)
	tMin := tOff
	if tMin < 0 {
		tMin = 0
	}
	tMax := int(ap.Input.TrialSteps) - tMin

	fMin := 0
	fMax := int(ap.MelFBank.NFilters) - spec.SizeFreq

	tIdx := 0
	for s := tMin; s < tMax; s, tIdx = spec.SpaceTime, tIdx+1 {
		inSt := s - tOff
		if tIdx > outRaw.Dim(3) {
			fmt.Printf("GaborFilter: time index %v out of range: %v", tIdx, outRaw.Dim(3))
			break
		}

		fIdx := 0
		for flt := fMin; flt < fMax; flt, fIdx = spec.SpaceFreq, fIdx+1 {
			if fIdx > outRaw.Dim(2) {
				fmt.Printf("GaborFilter: freq index %v out of range: %v", tIdx, outRaw.Dim(2))
				break
			}
			nf := spec.NFilters
			for fi := 0; fi < nf; fi++ {
				fSum := float32(0.0)
				for  ff := 0; ff < spec.SizeFreq; ff++ {
					for ft := 0; ft < spec.SizeTime; ft++ {
						fVal := filters.Value([]int{ft, ff, fi})
						iVal := ap.MelFBankTrialOut.Value([]int{flt + ff, inSt + ft, ch})
						fSum += fVal * iVal
					}
				}
				pos := fSum >= 0.0
				act := spec.Gain * math32.Abs(fSum)
				if(pos) {
					outRaw.SetFloat([]int{fi, 0, fIdx, tIdx, ch}, float64(act))
					outRaw.SetFloat([]int{fi, 1, fIdx, tIdx, ch}, 0)
				} else {
					outRaw.SetFloat([]int{fi, 0, fIdx, tIdx, ch}, 0)
					outRaw.SetFloat([]int{fi, 1, fIdx, tIdx, ch}, float64(act))
				}
			}
		}
	}

	rawFrm :=
float_MatrixPtr raw_frm; raw_frm = (float_Matrix*)out_raw.GetFrameSlice(chan);
float_MatrixPtr out_frm; out_frm = (float_Matrix*)out.GetFrameSlice(chan);

if(gabor_kwta.On()) {
gabor_kwta.Compute_Inhib(*raw_frm, *out_frm, gabor_gci);
}
else {
memcpy(out_frm->el, raw_frm->el, raw_frm->size * sizeof(float));
}

}

float_MatrixPtr raw_frm; raw_frm = (float_Matrix*)out_raw.GetFrameSlice(chan);
float_MatrixPtr out_frm; out_frm = (float_Matrix*)out.GetFrameSlice(chan);

if(gabor_kwta.On()) {
gabor_kwta.Compute_Inhib(*raw_frm, *out_frm, gabor_gci);
}
else {
memcpy(out_frm->el, raw_frm->el, raw_frm->size * sizeof(float));
}
}

// FilterWindow filters the current window_in input data according to current settings -- called by ProcessStep, but can be called separately
func (ap *AuditoryProc) FilterWindow(ch int, step int) bool {
	ap.DftInput(ch, step)
	if ap.MelFBank.On {
		ap.PowerOfDft(ch, step)
		ap.MelFilterDft(ch, step)
		if ap.Mfcc.On {
			ap.CepstrumDctMel(ch, step)
		}
	}
	return true
}

// OutputToTable
func (ap *AuditoryProc) OutputToTable(ch int) {
	if (!data_table || save_mode == NONE_SAVE) // bail now
	return false;
	data_table- > StructUpdate(true);
	if (mel_fbank.on) {
		MelOutputToTable(data_table, chan, false); // not fmt_only
	}
	data_table- > StructUpdate(false);
	return true;
}


// MelOutputToTable mel filter bank to output table
func (ap *AuditoryProc) MelOutputToTable(dt *dtable.Table, ch int, fmtOnly bool) bool { // ch is channel
	//var idx int
	var colSfx string

	if ap.Input.Channels > 1 {
		colSfx = "_ch" + strconv.Itoa(ch)
	}

	var err error
	cn := "AudProc" + "_dft_pow" + colSfx // column name
	col := dt.ColByName(cn)
	if col == nil {
		err = dt.AddCol(etensor.NewFloat32([]int{int(ap.Input.TotalSteps), int(ap.DftUse)}, nil, nil), cn)
		if err != nil {
			fmt.Printf("MelOutputToTable: column not found or failed to be created")
			return false
		}
	}

	col = dt.ColByName(cn)
	if fmtOnly == false {
		dout := col.CloneTensor()
		for s := 0; s < int(ap.Input.TotalSteps); s++ {
			for i := 0; i < int(ap.DftUse); i++ {
				if ap.Dft.LogPow {
					val := ap.DftLogPowerTrialOut.FloatVal([]int{i, s, ch})
					dout.SetFloat([]int{s, i}, val)
				} else {
					val := ap.DftPowerTrialOut.FloatVal([]int{i, s, ch})
					dout.SetFloat([]int{s, i}, val)
				}
			}
		}
	}

	if ap.MelFBank.On {
		cn := "AudProc" + "_mel_fbank" + colSfx // column name
		col := dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{int(ap.Input.TotalSteps), int(ap.MelFBank.NFilters)}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			for s := 0; s < int(ap.Input.TotalSteps); s++ {
				for i := 0; i < int(ap.MelFBank.NFilters); i++ {
					val := ap.MelFBankTrialOut.FloatVal([]int{i, s, ch})
					dout.SetFloat([]int{s, i}, val)
				}
			}
		}
	}

	if ap.Gabor1.On {
		cn := "AudProc" + "_mel_gabor1_raw" + colSfx // column name
		col := dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{ap.Gabor1.NFilters, 2, ap.Gabor1Shape.X, ap.Gabor1Shape.Y}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			nf := ap.Gabor1.NFilters
			for s := 0; s < ap.Gabor1Shape.X; s++ {
				for i := 0; i < ap.Gabor1Shape.Y; i++ {
					for ti := 0; ti < nf; ti++ {
						val0 := ap.Gabor1TrialRaw.FloatVal([]int{ti, 0, i, s, ch})
						dout.SetFloat([]int{ti, 0, s, i}, val0)
						val1 := ap.Gabor1TrialRaw.FloatVal([]int{ti, 1, i, s, ch})
						dout.SetFloat([]int{ti, 1, s, i}, val1)
					}
				}
			}
		}

		cn = "AudProc" + "_mel_gabor1" + colSfx // column name
		col = dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{ap.Gabor1.NFilters, 2, ap.Gabor1Shape.X, ap.Gabor1Shape.Y}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			nf := ap.Gabor1.NFilters
			for s := 0; s < ap.Gabor1Shape.X; s++ {
				for i := 0; i < ap.Gabor1Shape.Y; i++ {
					for ti := 0; ti < nf; ti++ {
						val0 := ap.Gabor1TrialOut.FloatVal([]int{ti, 0, i, s, ch})
						dout.SetFloat([]int{ti, 0, s, i}, val0)
						val1 := ap.Gabor1TrialOut.FloatVal([]int{ti, 1, i, s, ch})
						dout.SetFloat([]int{ti, 1, s, i}, val1)
					}
				}
			}
		}
	}

	if ap.Gabor2.On {
		cn := "AudProc" + "_mel_gabor2_raw" + colSfx // column name
		col := dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{ap.Gabor2.NFilters, 2, ap.Gabor2Shape.X, ap.Gabor2Shape.Y}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			nf := ap.Gabor2.NFilters
			for s := 0; s < ap.Gabor2Shape.X; s++ {
				for i := 0; i < ap.Gabor2Shape.Y; i++ {
					for ti := 0; ti < nf; ti++ {
						val0 := ap.Gabor2TrialRaw.FloatVal([]int{ti, 0, i, s, ch})
						dout.SetFloat([]int{ti, 0, s, i}, val0)
						val1 := ap.Gabor2TrialRaw.FloatVal([]int{ti, 1, i, s, ch})
						dout.SetFloat([]int{ti, 1, s, i}, val1)
					}
				}
			}
		}

		cn = "AudProc" + "_mel_gabor2" + colSfx // column name
		col = dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{ap.Gabor2.NFilters, 2, ap.Gabor2Shape.X, ap.Gabor2Shape.Y}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			nf := ap.Gabor2.NFilters
			for s := 0; s < ap.Gabor2Shape.X; s++ {
				for i := 0; i < ap.Gabor2Shape.Y; i++ {
					for ti := 0; ti < nf; ti++ {
						val0 := ap.Gabor2TrialOut.FloatVal([]int{ti, 0, i, s, ch})
						dout.SetFloat([]int{ti, 0, s, i}, val0)
						val1 := ap.Gabor2TrialOut.FloatVal([]int{ti, 1, i, s, ch})
						dout.SetFloat([]int{ti, 1, s, i}, val1)
					}
				}
			}
		}
	}

	if ap.Gabor3.On {
		cn := "AudProc" + "_mel_gabor3_raw" + colSfx // column name
		col := dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{ap.Gabor3.NFilters, 2, ap.Gabor3Shape.X, ap.Gabor3Shape.Y}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			nf := ap.Gabor3.NFilters
			for s := 0; s < ap.Gabor3Shape.X; s++ {
				for i := 0; i < ap.Gabor3Shape.Y; i++ {
					for ti := 0; ti < nf; ti++ {
						val0 := ap.Gabor3TrialRaw.FloatVal([]int{ti, 0, i, s, ch})
						dout.SetFloat([]int{ti, 0, s, i}, val0)
						val1 := ap.Gabor3TrialRaw.FloatVal([]int{ti, 1, i, s, ch})
						dout.SetFloat([]int{ti, 1, s, i}, val1)
					}
				}
			}
		}

		cn = "AudProc" + "_mel_gabor3" + colSfx // column name
		col = dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{ap.Gabor3.NFilters, 2, ap.Gabor3Shape.X, ap.Gabor3Shape.Y}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			nf := ap.Gabor3.NFilters
			for s := 0; s < ap.Gabor3Shape.X; s++ {
				for i := 0; i < ap.Gabor3Shape.Y; i++ {
					for ti := 0; ti < nf; ti++ {
						val0 := ap.Gabor3TrialOut.FloatVal([]int{ti, 0, i, s, ch})
						dout.SetFloat([]int{ti, 0, s, i}, val0)
						val1 := ap.Gabor3TrialOut.FloatVal([]int{ti, 1, i, s, ch})
						dout.SetFloat([]int{ti, 1, s, i}, val1)
					}
				}
			}
		}
	}

	if ap.Mfcc.On {
		cn = "AudProc" + "_mel_mfcc" + colSfx // column name
		col = dt.ColByName(cn)
		if col == nil {
			err = dt.AddCol(etensor.NewFloat32([]int{int(ap.Input.TotalSteps), ap.Mfcc.NCoeff}, nil, nil), cn)
			if err != nil {
				fmt.Printf("MelOutputToTable: column not found or failed to be created")
				return false
			}
		}
		col = dt.ColByName(cn)
		if fmtOnly == false {
			dout := col.CloneTensor()
			for s := 0; s < int(ap.Input.TotalSteps); s++ {
				for i := 0; i < ap.Mfcc.NCoeff; i {
					val := ap.MfccDctTrialOut.FloatVal([]int{i, s, ch})
					dout.SetFloat([]int{s, i}, val)
				}
			}
		}
	}
	return true
}
