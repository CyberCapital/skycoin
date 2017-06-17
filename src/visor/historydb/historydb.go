// Package historydb is in charge of parsing the consuses blokchain, and providing
// apis for blockchain explorer.
package historydb

import (
	"errors"

	"github.com/boltdb/bolt"
	"github.com/skycoin/skycoin/src/cipher"
	"github.com/skycoin/skycoin/src/coin"
)

// Blockchainer interface for isolating the detail of blockchain.
type Blockchainer interface {
	Head() *coin.Block
	GetBlockInDepth(dep uint64) *coin.Block
	ExecuteBlock(b *coin.Block) (coin.UxArray, error)
	CreateGenesisBlock(genAddress cipher.Address, genCoins, timestamp uint64) coin.Block
	VerifyTransaction(tx coin.Transaction) error
	GetBlock(hash cipher.SHA256) *coin.Block
}

// HistoryDB provides apis for blockchain explorer.
type HistoryDB struct {
	db           *bolt.DB      // bolt db instance.
	txns         *transactions // transactions bucket.
	outputs      *UxOuts       // outputs bucket.
	addrUx       *addressUx    // bucket which stores all UxOuts that address recved.
	*historyMeta               // stores history meta info
}

// New create historydb instance and create corresponding buckets if does not exist.
func New(db *bolt.DB) (*HistoryDB, error) {
	hd := HistoryDB{db: db}
	var err error

	hd.txns, err = newTransactionsBkt(db)
	if err != nil {
		return nil, err
	}

	// create the output instance
	hd.outputs, err = newOutputsBkt(db)
	if err != nil {
		return nil, err
	}

	// create the toAddressTx instance.
	hd.addrUx, err = newAddressUxBkt(db)
	if err != nil {
		return nil, err
	}

	hd.historyMeta, err = newHistoryMeta(db)
	if err != nil {
		return nil, err
	}

	return &hd, nil
}

// ProcessBlockchain process the blocks in the chain.
func (hd *HistoryDB) ProcessBlockchain(bc Blockchainer) error {
	depth := bc.Head().Seq()
	for i := uint64(0); i <= depth; i++ {
		b := bc.GetBlockInDepth(i)
		if err := hd.ProcessBlock(b); err != nil {
			return err
		}
	}
	return nil
}

// GetUxout get UxOut of specific uxID.
func (hd *HistoryDB) GetUxout(uxID cipher.SHA256) (*UxOut, error) {
	return hd.outputs.Get(uxID)
}

// ProcessBlock will index the transaction, outputs,etc.
func (hd *HistoryDB) ProcessBlock(b *coin.Block) error {
	if b == nil {
		return errors.New("process nil block")
	}

	// index the transactions
	for _, t := range b.Body.Transactions {
		tx := Transaction{
			Tx:       t,
			BlockSeq: b.Seq(),
		}
		if err := hd.txns.Add(&tx); err != nil {
			return err
		}

		// handle tx in, genesis transaction's vin is empty, so should be ignored.
		if b.Seq() > 0 {
			for _, in := range t.In {
				o, err := hd.outputs.Get(in)
				if err != nil {
					return err
				}
				// update output's spent block seq and txid.
				o.SpentBlockSeq = b.Seq()
				o.SpentTxID = t.Hash()
				if err := hd.outputs.Set(*o); err != nil {
					return err
				}
			}
		}

		// handle the tx out
		uxArray := coin.CreateUnspents(b.Head, t)
		for _, ux := range uxArray {
			uxOut := UxOut{
				Out: ux,
			}
			if err := hd.outputs.Set(uxOut); err != nil {
				return err
			}

			if err := hd.addrUx.Add(ux.Body.Address, ux.Hash()); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetTransaction get transaction by hash.
func (hd HistoryDB) GetTransaction(hash cipher.SHA256) (*Transaction, error) {
	return hd.txns.Get(hash)
}

// GetLastTxs gets the latest N transactions.
func (hd HistoryDB) GetLastTxs() ([]*Transaction, error) {
	txHashes := hd.txns.GetLastTxs()
	txs := make([]*Transaction, len(txHashes))
	for i, h := range txHashes {
		tx, err := hd.txns.Get(h)
		if err != nil {
			return []*Transaction{}, err
		}
		txs[i] = tx
	}
	return txs, nil
}

// GetAddrUxOuts get all uxout that the address affected.
func (hd HistoryDB) GetAddrUxOuts(address cipher.Address) ([]*UxOut, error) {
	hashes, err := hd.addrUx.Get(address)
	if err != nil {
		return []*UxOut{}, err
	}
	uxOuts := make([]*UxOut, len(hashes))
	for i, hash := range hashes {
		ux, err := hd.outputs.Get(hash)
		if err != nil {
			return []*UxOut{}, err
		}
		uxOuts[i] = ux
	}
	return uxOuts, nil
}
