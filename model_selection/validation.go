package modelselection

import (
	"runtime"
	"time"

	"github.com/pa-m/sklearn/base"
	"gonum.org/v1/gonum/mat"
)

// CrossValidateResult is the struct result of CrossValidate. it includes TestScore,FitTime,ScoreTime,Estimator
type CrossValidateResult struct {
	TestScore          []float64
	FitTime, ScoreTime []time.Duration
	Estimator          []base.Transformer
}

// CrossValidate Evaluate a score by cross-validation
// scorer is a func(Ytrue,Ypred) float64
// only mean_squared_error for now
// NJobs is the number of goroutines. if <=0, runtime.NumCPU is used
func CrossValidate(estimator base.Transformer, X, Y *mat.Dense, groups []int, scorer func(Ytrue, Ypred *mat.Dense) float64, cv Splitter, NJobs int) (res CrossValidateResult) {

	if NJobs <= 0 {
		NJobs = runtime.NumCPU()
	}
	NSplits := cv.GetNSplits(X, Y)
	if NJobs > NSplits {
		NJobs = NSplits
	}
	if cv == Splitter(nil) {
		cv = &KFold{NSplits: 3, Shuffle: true}
	}
	res.Estimator = make([]base.Transformer, NSplits)
	res.TestScore = make([]float64, NSplits)
	res.FitTime = make([]time.Duration, NSplits)
	res.ScoreTime = make([]time.Duration, NSplits)
	type structIn struct {
		iSplit int
		Split
	}
	type structOut struct {
		iSplit int
		score  float64
	}
	estimatorCloner := estimator.(base.TransformerCloner)
	NSamples, NFeatures := X.Dims()
	_, NOutputs := Y.Dims()
	processSplit := func(job int, Xjob, Yjob *mat.Dense, sin structIn) structOut {
		Xtrain, Xtest, Ytrain, Ytest := &mat.Dense{}, &mat.Dense{}, &mat.Dense{}, &mat.Dense{}
		trainLen, testLen := len(sin.Split.TrainIndex), len(sin.Split.TestIndex)
		Xtrain.SetRawMatrix(base.MatGeneralRowSlice(Xjob.RawMatrix(), 0, trainLen))
		Ytrain.SetRawMatrix(base.MatGeneralRowSlice(Yjob.RawMatrix(), 0, trainLen))
		Xtest.SetRawMatrix(base.MatGeneralRowSlice(Xjob.RawMatrix(), trainLen, trainLen+testLen))
		Ytest.SetRawMatrix(base.MatGeneralRowSlice(Yjob.RawMatrix(), trainLen, trainLen+testLen))
		for i0, i1 := range sin.Split.TrainIndex {
			Xtrain.SetRow(i0, X.RawRowView(i1))
			Ytrain.SetRow(i0, Y.RawRowView(i1))
		}
		for i0, i1 := range sin.Split.TestIndex {
			Xtest.SetRow(i0, X.RawRowView(i1))
			Ytest.SetRow(i0, Y.RawRowView(i1))
		}

		res.Estimator[sin.iSplit] = estimatorCloner.Clone()
		t0 := time.Now()
		res.Estimator[sin.iSplit].Fit(Xtrain, Ytrain)
		res.FitTime[sin.iSplit] = time.Since(t0)
		t0 = time.Now()
		_, Ypred := res.Estimator[sin.iSplit].Transform(Xtest, Ytest)
		score := scorer(Ytest, Ypred)
		res.ScoreTime[sin.iSplit] = time.Since(t0)
		//fmt.Printf("score for split %d is %g\n", sin.iSplit, score)
		return structOut{sin.iSplit, score}

	}
	if NJobs > 1 {
		/*var useChannels = false
		if useChannels {
			chin := make(chan structIn)
			chout := make(chan structOut)
			// launch workers
			for j := 0; j < NJobs; j++ {
				go func(job int) {
					var Xjob, Yjob = mat.NewDense(NSamples, NFeatures, nil), mat.NewDense(NSamples, NOutputs, nil)
					for sin := range chin {
						chout <- processSplit(job, Xjob, Yjob, sin)

					}
				}(j)
			}
			var isplit int
			for split := range cv.Split(X, Y) {
				chin <- structIn{isplit, split}
				isplit++
			}
			close(chin)
			for range res.TestScore {
				sout := <-chout
				res.TestScore[sout.iSplit] = sout.score
			}
			close(chout)
		} else*/{ // use workGroup
			var sin = make([]structIn, 0, NSplits)
			for split := range cv.Split(X, Y) {
				sin = append(sin, structIn{iSplit: len(sin), Split: split})
			}
			base.Parallelize(NJobs, NSplits, func(th, start, end int) {
				var Xjob, Yjob = mat.NewDense(NSamples, NFeatures, nil), mat.NewDense(NSamples, NOutputs, nil)
				for i := start; i < end; i++ {
					sout := processSplit(th, Xjob, Yjob, sin[i])
					res.TestScore[sout.iSplit] = sout.score
				}
			})
		}
	} else { // NJobs==1
		var Xjob, Yjob = mat.NewDense(NSamples, NFeatures, nil), mat.NewDense(NSamples, NOutputs, nil)
		var isplit int
		for split := range cv.Split(X, Y) {
			sout := processSplit(0, Xjob, Yjob, structIn{iSplit: isplit, Split: split})
			res.TestScore[sout.iSplit] = sout.score
			isplit++
		}

	}
	return
}
